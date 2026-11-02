package telemetry

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

// disk keeps metric samples and logs past a restart, for as long as the
// retention allows, in a bolt file of its own. It is apart from budge's own
// database so a backup of that carries none of this, and so what is written
// here is never itself traced as a query.
//
// Samples are written once a minute rather than every fifteen seconds: the
// finer samples stay in memory for the recent past, and a week drawn at a
// minute apart is already more points than a chart can show.
type disk struct {
	db        *bolt.DB
	retention time.Duration

	// Counting what the file holds walks all of it, so a count is reused for
	// a minute rather than taken on every look at the admin page.
	mu      sync.Mutex
	counted time.Time
	counts  Storage
}

var (
	seriesBucket = []byte("series")
	pointsBucket = []byte("points")
	logsBucket   = []byte("logs")
)

func openDisk(path string, retention time.Duration) (*disk, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{seriesBucket, pointsBucket, logsBucket} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &disk{db: db, retention: retention}, nil
}

func (d *disk) close() error { return d.db.Close() }

// sample is one series' value at a time, to be written down.
type sample struct {
	name  string
	key   string
	attrs map[string]string
	point Point
}

// A series is keyed by its metric's name and its attributes, and each of its
// points by that and the time, so a series' points sit together in time
// order and a range of them is one seek.
func seriesKey(name, attrs string) []byte {
	return append(append([]byte(name), 0), append([]byte(attrs), 0)...)
}

func timeKey(prefix []byte, at time.Time) []byte {
	key := append([]byte(nil), prefix...)
	return binary.BigEndian.AppendUint64(key, uint64(at.UnixNano()))
}

// write keeps samples and log records in one commit.
func (d *disk) write(samples []sample, records []LogRecord) error {
	if len(samples) == 0 && len(records) == 0 {
		return nil
	}
	return d.db.Update(func(tx *bolt.Tx) error {
		series, points, logs := tx.Bucket(seriesBucket), tx.Bucket(pointsBucket), tx.Bucket(logsBucket)
		for _, s := range samples {
			prefix := seriesKey(s.name, s.key)
			if series.Get(prefix) == nil {
				attrs, err := json.Marshal(s.attrs)
				if err != nil {
					return err
				}
				if err := series.Put(prefix, attrs); err != nil {
					return err
				}
			}
			value := binary.BigEndian.AppendUint64(nil, math.Float64bits(s.point.Value))
			if err := points.Put(timeKey(prefix, s.point.Time), value); err != nil {
				return err
			}
		}
		for _, record := range records {
			value, err := json.Marshal(record)
			if err != nil {
				return err
			}
			// Records logged in the same nanosecond are told apart by a
			// sequence the bucket hands out.
			seq, _ := logs.NextSequence()
			key := binary.BigEndian.AppendUint32(timeKey(nil, record.Time), uint32(seq))
			if err := logs.Put(key, value); err != nil {
				return err
			}
		}
		return nil
	})
}

// total sums a metric's series at each time in [since, before).
func (d *disk) total(name string, since, before time.Time) ([]Point, error) {
	byTime := map[int64]float64{}
	err := d.db.View(func(tx *bolt.Tx) error {
		series, points := tx.Bucket(seriesBucket).Cursor(), tx.Bucket(pointsBucket).Cursor()
		named := append([]byte(name), 0)
		for prefix, _ := series.Seek(named); prefix != nil && bytes.HasPrefix(prefix, named); prefix, _ = series.Next() {
			end := timeKey(prefix, before)
			for k, v := points.Seek(timeKey(prefix, since)); k != nil && bytes.Compare(k, end) < 0; k, v = points.Next() {
				at := int64(binary.BigEndian.Uint64(k[len(prefix):]))
				byTime[at] += math.Float64frombits(binary.BigEndian.Uint64(v))
			}
		}
		return nil
	})
	out := make([]Point, 0, len(byTime))
	for at, value := range byTime {
		out = append(out, Point{Time: time.Unix(0, at), Value: value})
	}
	sortPoints(out)
	return out, err
}

// errStop ends a walk over the logs early.
var errStop = errors.New("stop")

// logs walks the records logged before a time, newest first, until visit
// returns false.
func (d *disk) logs(before time.Time, visit func(LogRecord) bool) error {
	err := d.db.View(func(tx *bolt.Tx) error {
		c := tx.Bucket(logsBucket).Cursor()
		k, v := c.Seek(timeKey(nil, before))
		if k == nil {
			k, v = c.Last()
		} else {
			k, v = c.Prev()
		}
		for ; k != nil; k, v = c.Prev() {
			var record LogRecord
			if err := json.Unmarshal(v, &record); err != nil {
				continue
			}
			if !record.Time.Before(before) {
				continue
			}
			if !visit(record) {
				return errStop
			}
		}
		return nil
	})
	if errors.Is(err, errStop) {
		return nil
	}
	return err
}

// prune drops everything older than the retention.
func (d *disk) prune(now time.Time) error {
	cutoff := now.Add(-d.retention)
	return d.db.Update(func(tx *bolt.Tx) error {
		series, points := tx.Bucket(seriesBucket), tx.Bucket(pointsBucket)
		var emptied [][]byte
		err := series.ForEach(func(prefix, _ []byte) error {
			c := points.Cursor()
			end := timeKey(prefix, cutoff)
			for k, _ := c.Seek(prefix); k != nil && bytes.Compare(k, end) < 0; k, _ = c.Seek(prefix) {
				if err := c.Delete(); err != nil {
					return err
				}
			}
			if k, _ := c.Seek(prefix); k == nil || !bytes.HasPrefix(k, prefix) {
				emptied = append(emptied, append([]byte(nil), prefix...))
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, prefix := range emptied {
			if err := series.Delete(prefix); err != nil {
				return err
			}
		}

		end := timeKey(nil, cutoff)
		for _, name := range [][]byte{logsBucket, eventsBucket} {
			bucket := tx.Bucket(name)
			if bucket == nil {
				continue
			}
			c := bucket.Cursor()
			for k, _ := c.First(); k != nil && bytes.Compare(k, end) < 0; k, _ = c.First() {
				if err := c.Delete(); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// stats counts what the file holds, and how big it is.
func (d *disk) stats(s *Storage) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if time.Since(d.counted) >= time.Minute {
		err := d.db.View(func(tx *bolt.Tx) error {
			d.counts.FileBytes = tx.Size()
			d.counts.Series = tx.Bucket(seriesBucket).Stats().KeyN
			d.counts.Points = tx.Bucket(pointsBucket).Stats().KeyN
			d.counts.Logs = tx.Bucket(logsBucket).Stats().KeyN
			return nil
		})
		if err != nil {
			return err
		}
		d.counted = time.Now()
	}
	s.FileBytes, s.Series, s.Points, s.Logs = d.counts.FileBytes, d.counts.Series, d.counts.Points, d.counts.Logs
	return nil
}

// Event marks a run of budge starting or stopping, so restarts, and the
// version each run was, can be seen after the fact.
type Event struct {
	Time      time.Time
	Kind      string
	Version   string
	Revision  string
	GoVersion string
	Host      string
	PID       int
	Reason    string
}

const (
	EventStart = "start"
	EventStop  = "stop"
)

var eventsBucket = []byte("events")

func (d *disk) mark(event Event) error {
	value, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return d.db.Update(func(tx *bolt.Tx) error {
		events, err := tx.CreateBucketIfNotExists(eventsBucket)
		if err != nil {
			return err
		}
		seq, _ := events.NextSequence()
		return events.Put(binary.BigEndian.AppendUint32(timeKey(nil, event.Time), uint32(seq)), value)
	})
}

// events reads every start and stop kept, oldest first.
func (d *disk) events() ([]Event, error) {
	var out []Event
	err := d.db.View(func(tx *bolt.Tx) error {
		events := tx.Bucket(eventsBucket)
		if events == nil {
			return nil
		}
		return events.ForEach(func(_, v []byte) error {
			var event Event
			if err := json.Unmarshal(v, &event); err == nil {
				out = append(out, event)
			}
			return nil
		})
	})
	return out, err
}

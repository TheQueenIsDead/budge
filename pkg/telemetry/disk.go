package telemetry

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
)

// disk keeps metric samples, logs and traces past a restart, for as long as
// the retention allows, in a bolt file of its own. It is apart from budge's
// own database so a backup of that carries none of this, and so what is
// written here is never itself traced as a query.
//
// Samples are written once a minute rather than every fifteen seconds: the
// finer samples stay in memory for the recent past, and a week drawn at a
// minute apart is already more points than a chart can show.
type disk struct {
	path      string
	retention time.Duration

	// lock is held to read or write the file, and held alone to swap it for
	// a compacted copy, so nothing reaches a file being closed.
	lock sync.RWMutex
	db   *bolt.DB

	// Counting what the file holds walks all of it, so a count is reused for
	// a minute rather than taken on every look at the admin page.
	mu        sync.Mutex
	counted   time.Time
	counts    Storage
	compacted Compaction
}

var (
	seriesBucket = []byte("series")
	pointsBucket = []byte("points")
	logsBucket   = []byte("logs")
	eventsBucket = []byte("events")
	// A span is kept by its start, so it is pruned in time order; indexed by
	// its trace, so a trace is one seek; and, for a root, by its end, so the
	// latest requests and jobs read newest first.
	spansBucket = []byte("spans")
	tracesIndex = []byte("trace-spans")
	rootsBucket = []byte("roots")
)

func openDisk(path string, retention time.Duration) (*disk, error) {
	// A copy left by a compaction that never finished is not the file.
	_ = os.Remove(compactPath(path))
	db, err := openBolt(path)
	if err != nil {
		return nil, err
	}
	return &disk{path: path, db: db, retention: retention}, nil
}

func openBolt(path string) (*bolt.DB, error) {
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range [][]byte{seriesBucket, pointsBucket, logsBucket, eventsBucket, spansBucket, tracesIndex, rootsBucket} {
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
	return db, nil
}

func compactPath(path string) string { return path + ".compact" }

func (d *disk) close() error {
	d.lock.Lock()
	defer d.lock.Unlock()
	return d.db.Close()
}

func (d *disk) view(fn func(*bolt.Tx) error) error {
	d.lock.RLock()
	defer d.lock.RUnlock()
	return d.db.View(fn)
}

func (d *disk) update(fn func(*bolt.Tx) error) error {
	d.lock.RLock()
	defer d.lock.RUnlock()
	return d.db.Update(fn)
}

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

// write keeps samples, log records and spans in one commit.
func (d *disk) write(samples []sample, records []LogRecord, spans []Span) error {
	if len(samples) == 0 && len(records) == 0 && len(spans) == 0 {
		return nil
	}
	return d.update(func(tx *bolt.Tx) error {
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
		return putSpans(tx, spans)
	})
}

func putSpans(tx *bolt.Tx, spans []Span) error {
	stored, index, roots := tx.Bucket(spansBucket), tx.Bucket(tracesIndex), tx.Bucket(rootsBucket)
	for _, span := range spans {
		value, err := json.Marshal(span)
		if err != nil {
			return err
		}
		id := spanID(span.SpanID)
		key := append(timeKey(nil, span.Start), id...)
		if err := stored.Put(key, value); err != nil {
			return err
		}
		if err := index.Put(append(traceID(span.TraceID), id...), key); err != nil {
			return err
		}
		if span.ParentID == "" {
			if err := roots.Put(append(timeKey(nil, span.End()), id...), value); err != nil {
				return err
			}
		}
	}
	return nil
}

// IDs are kept as the bytes they are written in hex for, so keys stay short
// and sort the same way.
func traceID(id string) []byte {
	b, err := hex.DecodeString(id)
	if err != nil || len(b) != 16 {
		return make([]byte, 16)
	}
	return b
}

func spanID(id string) []byte {
	b, err := hex.DecodeString(id)
	if err != nil || len(b) != 8 {
		return make([]byte, 8)
	}
	return b
}

// trace reads every span kept for a trace.
func (d *disk) trace(id string) ([]Span, error) {
	var out []Span
	err := d.view(func(tx *bolt.Tx) error {
		stored, c := tx.Bucket(spansBucket), tx.Bucket(tracesIndex).Cursor()
		prefix := traceID(id)
		for k, key := c.Seek(prefix); k != nil && bytes.HasPrefix(k, prefix); k, key = c.Next() {
			var span Span
			if err := json.Unmarshal(stored.Get(key), &span); err == nil {
				out = append(out, span)
			}
		}
		return nil
	})
	return out, err
}

// roots walks the roots that ended before a time, newest first, until visit
// returns false.
func (d *disk) roots(before time.Time, visit func(Span) bool) error {
	return d.walkBack(rootsBucket, before, func(v []byte) bool {
		var span Span
		if err := json.Unmarshal(v, &span); err != nil {
			return true
		}
		return visit(span)
	})
}

// total sums a metric's series at each time in [since, before).
func (d *disk) total(name string, since, before time.Time) ([]Point, error) {
	byTime := map[int64]float64{}
	err := d.view(func(tx *bolt.Tx) error {
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

// errStop ends a walk early.
var errStop = errors.New("stop")

// logs walks the records logged before a time, newest first, until visit
// returns false.
func (d *disk) logs(before time.Time, visit func(LogRecord) bool) error {
	return d.walkBack(logsBucket, before, func(v []byte) bool {
		var record LogRecord
		if err := json.Unmarshal(v, &record); err != nil {
			return true
		}
		return visit(record)
	})
}

// walkBack visits a time-keyed bucket's values from before a time, newest
// first, until visit returns false.
func (d *disk) walkBack(bucket []byte, before time.Time, visit func([]byte) bool) error {
	err := d.view(func(tx *bolt.Tx) error {
		c := tx.Bucket(bucket).Cursor()
		k, v := c.Seek(timeKey(nil, before))
		if k == nil {
			k, v = c.Last()
		} else {
			k, v = c.Prev()
		}
		for ; k != nil; k, v = c.Prev() {
			if !visit(v) {
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
	return d.update(func(tx *bolt.Tx) error {
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
		index := tx.Bucket(tracesIndex)
		c := tx.Bucket(spansBucket).Cursor()
		for k, v := c.First(); k != nil && bytes.Compare(k, end) < 0; k, v = c.First() {
			var span Span
			if err := json.Unmarshal(v, &span); err == nil {
				if err := index.Delete(append(traceID(span.TraceID), spanID(span.SpanID)...)); err != nil {
					return err
				}
			}
			if err := c.Delete(); err != nil {
				return err
			}
		}
		for _, name := range [][]byte{logsBucket, eventsBucket, rootsBucket} {
			c := tx.Bucket(name).Cursor()
			for k, _ := c.First(); k != nil && bytes.Compare(k, end) < 0; k, _ = c.First() {
				if err := c.Delete(); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// stats counts what the file holds, how big it is, and how much of it is
// free space a compaction would give back.
func (d *disk) stats(s *Storage) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if time.Since(d.counted) >= time.Minute {
		err := d.view(func(tx *bolt.Tx) error {
			d.counts.FileBytes = d.fileSize()
			d.counts.Series = tx.Bucket(seriesBucket).Stats().KeyN
			d.counts.Points = tx.Bucket(pointsBucket).Stats().KeyN
			d.counts.Logs = tx.Bucket(logsBucket).Stats().KeyN
			d.counts.Spans = tx.Bucket(spansBucket).Stats().KeyN
			return nil
		})
		if err != nil {
			return err
		}
		d.counted = time.Now()
	}
	s.FileBytes, s.Series, s.Points, s.Logs, s.Spans = d.counts.FileBytes, d.counts.Series, d.counts.Points, d.counts.Logs, d.counts.Spans
	_, s.Reclaimable = d.reclaimable()
	s.Compacted = d.compacted
	return nil
}

// Compaction is a time the file was packed down, and what that gave back.
type Compaction struct {
	At     time.Time
	Before int64
	After  int64
	Took   time.Duration
}

// Freed is how much the compaction gave back.
func (c Compaction) Freed() int64 { return max(0, c.Before-c.After) }

// reclaimable is the file's size and how much of it is free pages: room
// pruning has made, which bolt reuses but never hands back on its own.
func (d *disk) reclaimable() (size, free int64) {
	d.lock.RLock()
	defer d.lock.RUnlock()
	return d.fileSize(), int64(d.db.Stats().FreeAlloc)
}

// fileSize is how much room the file takes on disk, which bolt grows ahead
// of what it holds.
func (d *disk) fileSize() int64 {
	info, err := os.Stat(d.path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// compactWhen is how much of the file must be free before it is worth
// compacting, and the least size a file must be for that to matter.
const (
	compactWhenFree = 1.0 / 3
	compactAtLeast  = 1 << 20
)

// worthCompacting reports whether enough of the file is free to be worth
// packing down.
func (d *disk) worthCompacting() bool {
	size, free := d.reclaimable()
	return size >= compactAtLeast && float64(free) >= compactWhenFree*float64(size)
}

// compact packs the file down: what is live is copied into a fresh file,
// which then takes the old one's place. Reads and writes wait while it runs,
// which for a file this size is a moment. The swap is a rename, so a crash
// part way leaves the old file whole, and the copy is cleared on next open.
func (d *disk) compact() (Compaction, error) {
	result, err := d.swap()
	if err != nil {
		return result, err
	}
	// Noted once the file is free again: stats holds this while it reads.
	d.mu.Lock()
	d.compacted, d.counted = result, time.Time{}
	d.mu.Unlock()
	return result, nil
}

func (d *disk) swap() (Compaction, error) {
	d.lock.Lock()
	defer d.lock.Unlock()
	started := time.Now()
	result := Compaction{At: started, Before: d.fileSize()}

	copyPath := compactPath(d.path)
	_ = os.Remove(copyPath)
	dst, err := bolt.Open(copyPath, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return result, err
	}
	if err := bolt.Compact(dst, d.db, 8<<20); err != nil {
		_ = dst.Close()
		_ = os.Remove(copyPath)
		return result, err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(copyPath)
		return result, err
	}
	if err := d.db.Close(); err != nil {
		return result, err
	}
	renamed := os.Rename(copyPath, d.path)
	// Whether or not the copy took its place, a file is reopened: the old one
	// if the rename failed, so telemetry carries on either way.
	db, err := openBolt(d.path)
	if err != nil {
		return result, errors.Join(renamed, err)
	}
	d.db = db
	if renamed != nil {
		return result, renamed
	}
	result.After = d.fileSize()
	result.Took = time.Since(started)
	return result, nil
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

func (d *disk) mark(event Event) error {
	value, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return d.update(func(tx *bolt.Tx) error {
		events := tx.Bucket(eventsBucket)
		seq, _ := events.NextSequence()
		return events.Put(binary.BigEndian.AppendUint32(timeKey(nil, event.Time), uint32(seq)), value)
	})
}

// events reads every start and stop kept, oldest first.
func (d *disk) events() ([]Event, error) {
	var out []Event
	err := d.view(func(tx *bolt.Tx) error {
		return tx.Bucket(eventsBucket).ForEach(func(_, v []byte) error {
			var event Event
			if err := json.Unmarshal(v, &event); err == nil {
				out = append(out, event)
			}
			return nil
		})
	})
	return out, err
}

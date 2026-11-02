package telemetry

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// Point is one sample of a counter or gauge.
type Point struct {
	Time  time.Time
	Value float64
}

// Series is a counter or gauge sampled over time, for one set of
// attributes.
type Series struct {
	Attrs  map[string]string
	Points []Point
}

// Histogram is a histogram's running totals since start, for one set of
// attributes: how many values fell in each bucket, bounded by Bounds.
type Histogram struct {
	Attrs  map[string]string
	Count  uint64
	Sum    float64
	Max    float64
	Bounds []float64
	Counts []uint64
}

// Mean is the average value recorded.
func (h Histogram) Mean() float64 {
	if h.Count == 0 {
		return 0
	}
	return h.Sum / float64(h.Count)
}

// Quantile estimates the value below which q of the recordings fall, by
// interpolating within the bucket it lands in.
func (h Histogram) Quantile(q float64) float64 {
	if h.Count == 0 {
		return 0
	}
	rank := q * float64(h.Count)
	var seen float64
	for i, n := range h.Counts {
		if n == 0 {
			continue
		}
		if seen+float64(n) >= rank {
			lower := 0.0
			if i > 0 {
				lower = h.Bounds[i-1]
			}
			// Nothing recorded exceeds the max, so a bucket runs no further.
			upper := h.Max
			if i < len(h.Bounds) {
				upper = min(h.Bounds[i], h.Max)
			}
			lower = min(lower, upper)
			return lower + (upper-lower)*(rank-seen)/float64(n)
		}
		seen += float64(n)
	}
	return h.Max
}

// Merge adds histograms with the same buckets together, as one.
func Merge(hs ...Histogram) Histogram {
	var out Histogram
	for _, h := range hs {
		if out.Counts == nil {
			out.Bounds = h.Bounds
			out.Counts = make([]uint64, len(h.Counts))
		}
		if len(h.Counts) != len(out.Counts) {
			continue
		}
		out.Count += h.Count
		out.Sum += h.Sum
		out.Max = max(out.Max, h.Max)
		for i, n := range h.Counts {
			out.Counts[i] += n
		}
	}
	return out
}

// CountSuffix names the series kept of how many values a histogram has
// recorded, so throughput can be charted over time: the request durations'
// is "http.server.request.duration.count".
const CountSuffix = ".count"

// MetricStore samples the SDK's metrics on an interval, keeping each counter
// and gauge as a series over a window, and each histogram's latest totals.
type MetricStore struct {
	reader *sdkmetric.ManualReader
	points int
	disk   *disk

	// persisted is the newest sample written to disk.
	persisted time.Time

	mu         sync.RWMutex
	sampling   bool
	series     map[string]map[string]*seriesBuffer
	histograms map[string][]Histogram
}

type seriesBuffer struct {
	attrs  map[string]string
	points *ring[Point]
}

func newMetricStore(reader *sdkmetric.ManualReader, window, every time.Duration) *MetricStore {
	return &MetricStore{
		reader:     reader,
		points:     max(1, int(window/every)),
		series:     map[string]map[string]*seriesBuffer{},
		histograms: map[string][]Histogram{},
	}
}

// Collect samples every metric once.
func (m *MetricStore) Collect(ctx context.Context) error {
	return m.collect(ctx, true)
}

// Refresh brings the histograms' totals up to date without taking a sample
// of the series, so looking at the page does not crowd the charts with
// points out of step with the rest.
func (m *MetricStore) Refresh(ctx context.Context) error {
	return m.collect(ctx, false)
}

func (m *MetricStore) collect(ctx context.Context, sample bool) error {
	var rm metricdata.ResourceMetrics
	if err := m.reader.Collect(ctx, &rm); err != nil {
		return err
	}
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sampling = sample
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			switch data := metric.Data.(type) {
			case metricdata.Sum[int64]:
				for _, p := range data.DataPoints {
					m.record(metric.Name, p.Attributes, now, float64(p.Value))
				}
			case metricdata.Sum[float64]:
				for _, p := range data.DataPoints {
					m.record(metric.Name, p.Attributes, now, p.Value)
				}
			case metricdata.Gauge[int64]:
				for _, p := range data.DataPoints {
					m.record(metric.Name, p.Attributes, now, float64(p.Value))
				}
			case metricdata.Gauge[float64]:
				for _, p := range data.DataPoints {
					m.record(metric.Name, p.Attributes, now, p.Value)
				}
			case metricdata.Histogram[float64]:
				var hs []Histogram
				for _, p := range data.DataPoints {
					m.record(metric.Name+CountSuffix, p.Attributes, now, float64(p.Count))
					max, _ := p.Max.Value()
					hs = append(hs, Histogram{
						Attrs: attrMap(p.Attributes), Count: p.Count, Sum: p.Sum, Max: max,
						Bounds: p.Bounds, Counts: p.BucketCounts,
					})
				}
				m.histograms[metric.Name] = hs
			case metricdata.Histogram[int64]:
				var hs []Histogram
				for _, p := range data.DataPoints {
					m.record(metric.Name+CountSuffix, p.Attributes, now, float64(p.Count))
					max, _ := p.Max.Value()
					hs = append(hs, Histogram{
						Attrs: attrMap(p.Attributes), Count: p.Count, Sum: float64(p.Sum), Max: float64(max),
						Bounds: p.Bounds, Counts: p.BucketCounts,
					})
				}
				m.histograms[metric.Name] = hs
			}
		}
	}
	return nil
}

func (m *MetricStore) record(name string, attrs attribute.Set, at time.Time, value float64) {
	if !m.sampling {
		return
	}
	byAttrs := m.series[name]
	if byAttrs == nil {
		byAttrs = map[string]*seriesBuffer{}
		m.series[name] = byAttrs
	}
	key := attrs.Encoded(attribute.DefaultEncoder())
	buffer := byAttrs[key]
	if buffer == nil {
		buffer = &seriesBuffer{attrs: attrMap(attrs), points: newRing[Point](m.points)}
		byAttrs[key] = buffer
	}
	buffer.points.add(Point{Time: at, Value: value})
}

// Series returns a counter or gauge over the window, oldest point first, for
// each set of attributes it was recorded with.
func (m *MetricStore) Series(name string) []Series {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Series
	for _, buffer := range m.series[name] {
		points := buffer.points.all()
		for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
			points[i], points[j] = points[j], points[i]
		}
		out = append(out, Series{Attrs: buffer.attrs, Points: points})
	}
	sort.Slice(out, func(i, j int) bool { return attrKey(out[i].Attrs) < attrKey(out[j].Attrs) })
	return out
}

// History sums a metric's series across their attributes at each sample
// since a time: from memory for as far back as it reaches, and from disk
// before that.
func (m *MetricStore) History(name string, since time.Time) []Point {
	recent := m.Total(name)
	if m.disk == nil {
		return after(recent, since)
	}
	before := time.Now()
	if len(recent) > 0 {
		before = recent[0].Time
	}
	if !since.Before(before) {
		return after(recent, since)
	}
	older, err := m.disk.total(name, since, before)
	if err != nil {
		slog.Warn("could not read metrics from disk", "metric", name, "error", err)
	}
	return append(older, after(recent, since)...)
}

func after(points []Point, since time.Time) []Point {
	for i, p := range points {
		if !p.Time.Before(since) {
			return points[i:]
		}
	}
	return nil
}

// Oldest is the earliest sample still in memory; before it, samples come
// from disk, a minute or so apart rather than a sample's interval.
func (m *MetricStore) Oldest() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var oldest time.Time
	for _, byAttrs := range m.series {
		for _, buffer := range byAttrs {
			points := buffer.points.all()
			if n := len(points); n > 0 && (oldest.IsZero() || points[n-1].Time.Before(oldest)) {
				oldest = points[n-1].Time
			}
		}
	}
	return oldest
}

// counts is how many series and points are kept in memory.
func (m *MetricStore) counts() (series, points int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, byAttrs := range m.series {
		for _, buffer := range byAttrs {
			series++
			points += buffer.points.len()
		}
	}
	return series, points
}

// unpersisted gathers each series' newest sample not yet on disk.
func (m *MetricStore) unpersisted() []sample {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []sample
	var newest time.Time
	for name, byAttrs := range m.series {
		for key, buffer := range byAttrs {
			points := buffer.points.all()
			if len(points) == 0 || !points[0].Time.After(m.persisted) {
				continue
			}
			out = append(out, sample{name: name, key: key, attrs: buffer.attrs, point: points[0]})
			if points[0].Time.After(newest) {
				newest = points[0].Time
			}
		}
	}
	return out
}

// markPersisted notes the newest sample written to disk.
func (m *MetricStore) markPersisted(samples []sample) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range samples {
		if s.point.Time.After(m.persisted) {
			m.persisted = s.point.Time
		}
	}
}

// Total sums a metric's series across their attributes at each sample kept
// in memory.
func (m *MetricStore) Total(name string) []Point {
	byTime := map[time.Time]float64{}
	for _, series := range m.Series(name) {
		for _, p := range series.Points {
			byTime[p.Time] += p.Value
		}
	}
	out := make([]Point, 0, len(byTime))
	for at, value := range byTime {
		out = append(out, Point{Time: at, Value: value})
	}
	sortPoints(out)
	return out
}

func sortPoints(points []Point) {
	sort.Slice(points, func(i, j int) bool { return points[i].Time.Before(points[j].Time) })
}

// Resample thins points to at most n, keeping the last of each stretch of
// time, so a week of samples draws as a chart rather than as noise. The last
// is right for a running total, whose rate is then taken across stretches,
// and near enough for a gauge.
func Resample(points []Point, n int) []Point {
	if len(points) <= n || n < 2 {
		return points
	}
	// n-1 stretches from the first to the last, which falls in one more.
	first, last := points[0].Time, points[len(points)-1].Time
	width := last.Sub(first) / time.Duration(n-1)
	if width <= 0 {
		return points[len(points)-n:]
	}
	stretch := func(p Point) time.Duration { return p.Time.Sub(first) / width }
	out := make([]Point, 0, n)
	for i, p := range points {
		if i+1 == len(points) || stretch(points[i+1]) != stretch(p) {
			out = append(out, p)
		}
	}
	return out
}

// Rate turns a running total into how fast it grew, per second, between each
// sample and the one before. A total that went backwards, as one does when it
// gains a new set of attributes partway, counts as no growth.
func Rate(points []Point) []Point {
	if len(points) < 2 {
		return nil
	}
	out := make([]Point, 0, len(points)-1)
	for i := 1; i < len(points); i++ {
		elapsed := points[i].Time.Sub(points[i-1].Time).Seconds()
		if elapsed <= 0 {
			continue
		}
		out = append(out, Point{Time: points[i].Time, Value: max(0, points[i].Value-points[i-1].Value) / elapsed})
	}
	return out
}

// Histograms returns a histogram's latest totals for each set of attributes.
func (m *MetricStore) Histograms(name string) []Histogram {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Histogram(nil), m.histograms[name]...)
}

// Names lists every metric sampled so far, for finding what is there.
func (m *MetricStore) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var names []string
	for name := range m.series {
		names = append(names, name)
	}
	for name := range m.histograms {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func attrMap(set attribute.Set) map[string]string {
	out := make(map[string]string, set.Len())
	for _, kv := range set.ToSlice() {
		out[string(kv.Key)] = kv.Value.Emit()
	}
	return out
}

func attrKey(attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for k, v := range attrs {
		keys = append(keys, k+"="+v)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

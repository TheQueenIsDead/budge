package telemetry

import (
	"context"
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
			upper := h.Max
			if i < len(h.Bounds) {
				upper = h.Bounds[i]
			}
			if upper < lower {
				upper = lower
			}
			return lower + (upper-lower)*(rank-seen)/float64(n)
		}
		seen += float64(n)
	}
	return h.Max
}

// MetricStore samples the SDK's metrics on an interval, keeping each counter
// and gauge as a series over a window, and each histogram's latest totals.
type MetricStore struct {
	reader *sdkmetric.ManualReader
	points int

	mu         sync.RWMutex
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
	var rm metricdata.ResourceMetrics
	if err := m.reader.Collect(ctx, &rm); err != nil {
		return err
	}
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
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

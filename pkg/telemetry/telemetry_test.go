package telemetry

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// noCollector clears the OTLP endpoints the environment may name, such as the
// one BuildKit gives a Dockerfile's RUN steps, so the tests export nowhere.
func noCollector(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
	} {
		t.Setenv(name, "")
	}
}

func TestEachSignalIsExportedOnItsOwn(t *testing.T) {
	for _, c := range []struct {
		name string
		env  map[string]string
		want exports
	}{
		{"nothing set", nil, exports{}},
		{"the generic endpoint", map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318"}, exports{true, true, true}},
		{"a traces endpoint alone", map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://tempo:4318/v1/traces"}, exports{traces: true}},
		{"a logs endpoint alone", map[string]string{"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": "http://loki:4318/v1/logs"}, exports{logs: true}},
		{"the generic endpoint, metrics turned off", map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318",
			"OTEL_METRICS_EXPORTER":       " None ",
		}, exports{traces: true, logs: true}},
		{"an exporter named with nowhere to send", map[string]string{"OTEL_TRACES_EXPORTER": "otlp"}, exports{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			noCollector(t)
			for _, signal := range []string{"TRACES", "METRICS", "LOGS"} {
				t.Setenv("OTEL_"+signal+"_EXPORTER", "")
			}
			for name, value := range c.env {
				t.Setenv(name, value)
			}
			got := otlpExports()
			assert.Equal(t, c.want, got)
			assert.Equal(t, c.want != exports{}, got.any())
		})
	}
}

func setup(t *testing.T, runtime bool) *Telemetry {
	t.Helper()
	noCollector(t)
	opts := DefaultOptions()
	opts.Stdout = io.Discard
	opts.Spans, opts.Logs = 10, 10
	opts.SampleEvery = time.Hour
	opts.Runtime = runtime
	tel, err := Setup(context.Background(), opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tel.Shutdown(context.Background()) })
	return tel
}

func TestSpansAreKept(t *testing.T) {
	tel := setup(t, false)
	tracer := otel.Tracer("test")

	ctx, request := tracer.Start(context.Background(), "GET /budget")
	_, query := tracer.Start(ctx, "bbolt read transactions")
	query.End()
	request.SetStatus(codes.Error, "boom")
	request.End()

	spans := tel.Spans.Recent(nil)
	require.Len(t, spans, 2)
	assert.Equal(t, "GET /budget", spans[0].Name, "newest first")
	assert.True(t, spans[0].Error)
	assert.Equal(t, "boom", spans[0].Status)

	trace := tel.Spans.Trace(spans[0].TraceID)
	require.Len(t, trace, 2)
	assert.Equal(t, "GET /budget", trace[0].Name, "a trace reads earliest first")
	assert.Equal(t, trace[0].SpanID, trace[1].ParentID, "the query sits under the request")
}

func TestLogsAreKeptAndWrittenOut(t *testing.T) {
	tel := setup(t, false)
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "work")
	slog.InfoContext(ctx, "synced", "transactions", 12)
	slog.ErrorContext(ctx, "sync failed", "error", "timeout")
	span.End()

	all := tel.Logs.Recent(otellog.SeverityDebug)
	require.Len(t, all, 3)
	assert.Equal(t, "budge started", all[2].Message, "starting is logged first")
	assert.Equal(t, "sync failed", all[0].Message)
	assert.Equal(t, "ERROR", all[0].Level)
	assert.True(t, all[0].IsError())
	assert.Equal(t, "timeout", all[0].Attrs["error"])
	assert.Equal(t, "12", all[1].Attrs["transactions"])
	assert.NotEmpty(t, all[0].TraceID, "a log made within a span names its trace")

	errs := tel.Logs.Recent(otellog.SeverityError)
	assert.Len(t, errs, 1)
}

func TestMetricsAreSampled(t *testing.T) {
	tel := setup(t, true)
	meter := otel.Meter("test")
	counter, err := meter.Int64Counter("budge.test.count")
	require.NoError(t, err)
	histogram, err := meter.Float64Histogram("budge.test.duration")
	require.NoError(t, err)

	counter.Add(context.Background(), 3)
	for _, v := range []float64{1, 2, 3, 4, 100} {
		histogram.Record(context.Background(), v)
	}
	require.NoError(t, tel.Metrics.Collect(context.Background()))
	counter.Add(context.Background(), 2)
	require.NoError(t, tel.Metrics.Collect(context.Background()))

	series := tel.Metrics.Series("budge.test.count")
	require.Len(t, series, 1)
	require.Len(t, series[0].Points, 2)
	assert.Equal(t, 3.0, series[0].Points[0].Value, "oldest first")
	assert.Equal(t, 5.0, series[0].Points[1].Value)

	hs := tel.Metrics.Histograms("budge.test.duration")
	require.Len(t, hs, 1)
	assert.Equal(t, uint64(5), hs[0].Count)
	assert.InDelta(t, 22, hs[0].Mean(), 0.001)
	assert.Equal(t, 100.0, hs[0].Max)
	assert.Less(t, hs[0].Quantile(0.5), 10.0)

	t.Logf("metrics sampled: %v", tel.Metrics.Names())
}

func TestRingKeepsTheNewest(t *testing.T) {
	r := newRing[int](3)
	r.add(1, 2)
	assert.Equal(t, []int{2, 1}, r.all())
	r.add(3, 4, 5)
	assert.Equal(t, []int{5, 4, 3}, r.all())
}

func TestHistogramQuantile(t *testing.T) {
	h := Histogram{Count: 100, Sum: 1000, Max: 50, Bounds: []float64{5, 10, 25}, Counts: []uint64{50, 40, 9, 1}}
	assert.InDelta(t, 5, h.Quantile(0.5), 0.001, "the 50th lands at the top of the first bucket")
	assert.InDelta(t, 9.375, h.Quantile(0.85), 0.001, "35 of the second bucket's 40, interpolated within it")
	assert.InDelta(t, 50, h.Quantile(1), 0.001, "the last bucket runs to the max")
	assert.Zero(t, Histogram{}.Quantile(0.5))

	quick := Histogram{Count: 2, Sum: 0.6, Max: 0.4, Bounds: []float64{5, 10}, Counts: []uint64{2, 0, 0}}
	assert.LessOrEqual(t, quick.Quantile(0.95), 0.4, "no estimate runs past the slowest recorded")
}

func TestMergeAddsHistogramsTogether(t *testing.T) {
	bounds := []float64{1, 10}
	merged := Merge(
		Histogram{Count: 2, Sum: 3, Max: 2, Bounds: bounds, Counts: []uint64{1, 1, 0}},
		Histogram{Count: 1, Sum: 20, Max: 20, Bounds: bounds, Counts: []uint64{0, 0, 1}},
	)
	assert.Equal(t, uint64(3), merged.Count)
	assert.Equal(t, 23.0, merged.Sum)
	assert.Equal(t, 20.0, merged.Max)
	assert.Equal(t, []uint64{1, 1, 1}, merged.Counts)
	assert.Zero(t, Merge().Count)
}

func TestRateIsGrowthPerSecond(t *testing.T) {
	at := time.Unix(0, 0)
	rate := Rate([]Point{{at, 10}, {at.Add(10 * time.Second), 30}, {at.Add(20 * time.Second), 5}})
	require.Len(t, rate, 2)
	assert.Equal(t, 2.0, rate[0].Value)
	assert.Zero(t, rate[1].Value, "a total that went backwards is no growth")
	assert.Nil(t, Rate([]Point{{at, 1}}))
}

func TestTotalSumsAcrossAttributes(t *testing.T) {
	tel := setup(t, false)
	counter, err := otel.Meter("test").Int64Counter("budge.test.split")
	require.NoError(t, err)
	counter.Add(context.Background(), 2, metric.WithAttributes(attribute.String("kind", "a")))
	counter.Add(context.Background(), 3, metric.WithAttributes(attribute.String("kind", "b")))
	require.NoError(t, tel.Metrics.Collect(context.Background()))

	total := tel.Metrics.Total("budge.test.split")
	require.Len(t, total, 1)
	assert.Equal(t, 5.0, total[0].Value)
}

func TestRefreshUpdatesHistogramsWithoutSampling(t *testing.T) {
	tel := setup(t, false)
	histogram, err := otel.Meter("test").Float64Histogram("budge.test.refresh")
	require.NoError(t, err)
	histogram.Record(context.Background(), 1)
	require.NoError(t, tel.Metrics.Refresh(context.Background()))

	require.Len(t, tel.Metrics.Histograms("budge.test.refresh"), 1)
	assert.Empty(t, tel.Metrics.Series("budge.test.refresh"+CountSuffix), "a refresh takes no sample")

	require.NoError(t, tel.Metrics.Collect(context.Background()))
	assert.Len(t, tel.Metrics.Series("budge.test.refresh"+CountSuffix), 1, "a sample keeps the histogram's count over time")
}

func TestRequestsOutlastTheirQueries(t *testing.T) {
	tel := setup(t, false) // keeps 10 roots, 40 children
	tracer := otel.Tracer("test")
	for range 3 {
		ctx, request := tracer.Start(context.Background(), "GET /", trace.WithSpanKind(trace.SpanKindServer))
		for range 30 {
			_, query := tracer.Start(ctx, "query")
			query.End()
		}
		request.End()
	}
	assert.Len(t, tel.Spans.Requests(), 3, "ninety queries do not push three requests out")
	assert.Len(t, tel.Spans.Recent(nil), 43)
}

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
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
)

func setup(t *testing.T, runtime bool) *Telemetry {
	t.Helper()
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
	require.Len(t, all, 2)
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
}

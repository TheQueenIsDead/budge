package telemetry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
)

// persisted sets telemetry up keeping its samples and logs in a file.
func persisted(t *testing.T, path string) *Telemetry {
	t.Helper()
	opts := DefaultOptions()
	opts.Stdout, opts.Runtime, opts.SampleEvery = io.Discard, false, time.Hour
	opts.Path = path
	tel, err := Setup(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, tel.disk, "the file opens")
	return tel
}

func TestSamplesAndLogsOutliveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.db")
	first := persisted(t, path)
	counter, err := otel.Meter("test").Int64Counter("budge.test.kept")
	require.NoError(t, err)
	counter.Add(context.Background(), 4)
	require.NoError(t, first.Metrics.Collect(context.Background()))
	slog.Error("sync failed", "error", "timeout")
	first.StoppingBecause("interrupted")
	require.NoError(t, first.Shutdown(context.Background()), "shutting down writes what is waiting")

	second := persisted(t, path)
	t.Cleanup(func() { _ = second.Shutdown(context.Background()) })

	history := second.Metrics.History("budge.test.kept", time.Now().Add(-time.Hour))
	require.Len(t, history, 1, "the last run's sample is read back from disk")
	assert.Equal(t, 4.0, history[0].Value)

	errs, total := second.Logs.Search(otellog.SeverityError, nil, 10)
	require.Equal(t, 1, total)
	assert.Equal(t, "sync failed", errs[0].Message, "the last run's error is still there")

	runs := second.Runs()
	require.Len(t, runs, 2)
	assert.True(t, runs[0].Current, "newest first")
	assert.True(t, runs[1].Stopped)
	assert.Equal(t, "interrupted", runs[1].Stop.Reason)
	assert.NotEmpty(t, runs[1].Start.Version)
	assert.False(t, runs[1].Ended())

	storage := second.Storage()
	assert.Positive(t, storage.FileBytes)
	assert.Equal(t, 3, storage.Logs, "the error, and starting and stopping")
	assert.Positive(t, storage.Points)
}

func TestARunThatNeverStoppedEnded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.db")
	crashed := persisted(t, path)
	// Gone without a word: nothing marks the stop.
	crashed.stop()
	<-crashed.sampling
	require.NoError(t, crashed.disk.close())

	next := persisted(t, path)
	t.Cleanup(func() { _ = next.Shutdown(context.Background()) })
	runs := next.Runs()
	require.Len(t, runs, 2)
	assert.True(t, runs[1].Ended(), "a start with no stop is a run that went down")
}

func TestPruneDropsWhatIsPastKeeping(t *testing.T) {
	d, err := openDisk(filepath.Join(t.TempDir(), "telemetry.db"), time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.close() })
	now := time.Now()
	old, recent := now.Add(-2*time.Hour), now.Add(-time.Minute)
	require.NoError(t, d.write(
		[]sample{
			{name: "m", key: "a", point: Point{old, 1}},
			{name: "m", key: "a", point: Point{recent, 2}},
			{name: "gone", key: "", point: Point{old, 3}},
		},
		[]LogRecord{{Time: old, Message: "old"}, {Time: recent, Message: "recent"}},
		nil,
	))
	require.NoError(t, d.mark(Event{Time: old, Kind: EventStart}))

	require.NoError(t, d.prune(now))

	points, err := d.total("m", now.Add(-24*time.Hour), now)
	require.NoError(t, err)
	require.Len(t, points, 1)
	assert.Equal(t, 2.0, points[0].Value)

	var s Storage
	require.NoError(t, d.stats(&s))
	assert.Equal(t, 1, s.Series, "a series left with nothing is dropped")
	assert.Equal(t, 1, s.Logs)
	events, err := d.events()
	require.NoError(t, err)
	assert.Empty(t, events)
}

func TestResampleKeepsTheLastOfEachStretch(t *testing.T) {
	at := time.Unix(0, 0)
	var points []Point
	for i := range 100 {
		points = append(points, Point{at.Add(time.Duration(i) * time.Minute), float64(i)})
	}
	thinned := Resample(points, 10)
	assert.LessOrEqual(t, len(thinned), 10)
	assert.Equal(t, 99.0, thinned[len(thinned)-1].Value, "the newest point survives")
	assert.Equal(t, points, Resample(points, 200), "few enough are left alone")
}

func TestTracesOutliveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.db")
	first := persisted(t, path)
	tracer := otel.Tracer("test")
	ctx, request := tracer.Start(context.Background(), "GET /budget", trace.WithSpanKind(trace.SpanKindServer))
	_, query := tracer.Start(ctx, "ReadTransactions transactions")
	query.End()
	request.RecordError(errors.New("boom"), trace.WithStackTrace(true))
	request.SetStatus(codes.Error, "boom")
	request.End()
	id := request.SpanContext().TraceID().String()
	require.NoError(t, first.Shutdown(context.Background()))

	second := persisted(t, path)
	t.Cleanup(func() { _ = second.Shutdown(context.Background()) })
	require.Empty(t, second.Spans.Recent(nil), "nothing of the last run is in memory")

	spans := second.Spans.Trace(id)
	require.Len(t, spans, 2, "the waterfall is read back from disk")
	assert.Equal(t, "GET /budget", spans[0].Name)
	assert.Equal(t, spans[0].SpanID, spans[1].ParentID)
	exceptions := spans[0].Exceptions()
	require.Len(t, exceptions, 1, "the exception is kept with its span")
	assert.Equal(t, "boom", exceptions[0].Message)
	assert.Equal(t, "*errors.errorString", exceptions[0].Type)
	assert.Contains(t, exceptions[0].Stack, "TestTracesOutliveARestart", "and the stack it was recorded with")

	var roots []Span
	second.Spans.History(func(span Span) bool { roots = append(roots, span); return true })
	require.Len(t, roots, 1, "the last run's request is listed, and its query is not a root")
	assert.True(t, roots[0].Error)
}

func TestPruneDropsOldSpansAndTheirIndex(t *testing.T) {
	d, err := openDisk(filepath.Join(t.TempDir(), "telemetry.db"), time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.close() })
	now := time.Now()
	span := func(trace, id, parent string, start time.Time) Span {
		return Span{TraceID: trace, SpanID: id, ParentID: parent, Start: start, Duration: time.Millisecond}
	}
	oldTrace, newTrace := "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"
	require.NoError(t, d.write(nil, nil, []Span{
		span(oldTrace, "0000000000000001", "", now.Add(-2*time.Hour)),
		span(oldTrace, "0000000000000002", "0000000000000001", now.Add(-2*time.Hour)),
		span(newTrace, "0000000000000003", "", now.Add(-time.Minute)),
	}))

	require.NoError(t, d.prune(now))

	gone, err := d.trace(oldTrace)
	require.NoError(t, err)
	assert.Empty(t, gone)
	kept, err := d.trace(newTrace)
	require.NoError(t, err)
	assert.Len(t, kept, 1)
	require.NoError(t, d.view(func(tx *bolt.Tx) error {
		assert.Equal(t, 1, tx.Bucket(tracesIndex).Stats().KeyN, "the index goes with the spans")
		assert.Equal(t, 1, tx.Bucket(rootsBucket).Stats().KeyN)
		return nil
	}))
}

func TestCompactGivesBackWhatPruningFreed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.db")
	d, err := openDisk(path, time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.close() })

	// A few megabytes of logs, most of them past keeping.
	now := time.Now()
	var records []LogRecord
	for i := range 20000 {
		at := now.Add(-2 * time.Hour)
		if i%10 == 0 {
			at = now.Add(-time.Minute)
		}
		records = append(records, LogRecord{Time: at.Add(time.Duration(i)), Message: strings.Repeat("x", 200)})
	}
	require.NoError(t, d.write(nil, records, nil))
	require.NoError(t, d.prune(now))
	require.True(t, d.worthCompacting(), "nine in ten records went, which leaves most of the file free")

	result, err := d.compact()
	require.NoError(t, err)
	assert.Less(t, result.After, result.Before/2, "the file shrinks")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, result.After, info.Size())
	assert.NoFileExists(t, compactPath(path), "the copy took the file's place")

	var kept int
	require.NoError(t, d.logs(now.Add(time.Hour), func(LogRecord) bool { kept++; return true }))
	assert.Equal(t, 2000, kept, "what was live is all still there")
	require.NoError(t, d.write(nil, []LogRecord{{Time: now, Message: "after"}}, nil), "and the file takes writes")
	assert.False(t, d.worthCompacting())
}

func TestALeftoverCompactionCopyIsCleared(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.db")
	require.NoError(t, os.WriteFile(compactPath(path), []byte("half written"), 0600))
	d, err := openDisk(path, time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.close() })
	assert.NoFileExists(t, compactPath(path))
}

func TestWritesWaitOutACompaction(t *testing.T) {
	d, err := openDisk(filepath.Join(t.TempDir(), "telemetry.db"), time.Hour)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.close() })

	done := make(chan error)
	go func() {
		var errs []error
		for i := range 200 {
			errs = append(errs, d.write(nil, []LogRecord{{Time: time.Now(), Message: strconv.Itoa(i)}}, nil))
		}
		done <- errors.Join(errs...)
	}()
	for range 5 {
		_, err := d.compact()
		require.NoError(t, err)
	}
	require.NoError(t, <-done, "no write reaches a file being swapped")

	var kept int
	require.NoError(t, d.logs(time.Now().Add(time.Hour), func(LogRecord) bool { kept++; return true }))
	assert.Equal(t, 200, kept)
}

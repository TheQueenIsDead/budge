package telemetry

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	otellog "go.opentelemetry.io/otel/log"
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

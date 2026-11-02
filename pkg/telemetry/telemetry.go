// Package telemetry instruments budge with OpenTelemetry, and keeps what it
// records in memory for the admin page: the recent spans, logs and metrics
// since the process started. When the standard OTLP environment variables
// are set, the same data is also exported to a collector.
package telemetry

import (
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	goruntime "runtime"
	"runtime/debug"
	"slices"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/host"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// Options says what to keep and for how long.
type Options struct {
	ServiceName string
	Version     string

	// Spans and Logs are how many of each are kept.
	Spans int
	Logs  int

	// SampleEvery is how often metrics are sampled, and Window how long a
	// sampled series is kept.
	SampleEvery time.Duration
	Window      time.Duration

	// Stdout is where logs are written as text, besides being kept.
	Stdout io.Writer

	// Runtime starts the Go runtime and process metrics.
	Runtime bool

	// Path is the bolt file metric samples and logs are kept in past a
	// restart, for Retention, written every PersistEvery. Without a path, or
	// with no retention, nothing outlives the process.
	Path         string
	Retention    time.Duration
	PersistEvery time.Duration
}

// DefaultOptions keeps six hours of metrics sampled every 15 seconds in
// memory, and the last few thousand spans and logs. Given a path, a week of
// samples and logs is kept on disk.
func DefaultOptions() Options {
	return Options{
		ServiceName:  "budge",
		Spans:        5000,
		Logs:         5000,
		SampleEvery:  15 * time.Second,
		Window:       6 * time.Hour,
		Stdout:       os.Stdout,
		Runtime:      true,
		Retention:    7 * 24 * time.Hour,
		PersistEvery: time.Minute,
	}
}

// Telemetry is what has been recorded since the process started.
type Telemetry struct {
	Started time.Time
	Spans   *SpanStore
	Logs    *LogStore
	Metrics *MetricStore

	// Exporting is whether the same data is also sent to a collector.
	Exporting bool

	// SampleEvery is how often metrics are sampled, and Window how far back
	// those samples are kept in memory.
	SampleEvery time.Duration
	Window      time.Duration

	// Retention is how long samples and logs are kept on disk, and Path the
	// file they are kept in, a sample every PersistEvery. Without one,
	// Retention is zero.
	Retention    time.Duration
	Path         string
	PersistEvery time.Duration

	// Run is this run's start, as marked on disk.
	Run Event

	stopReason string

	disk     *disk
	sampling chan struct{}

	stop     context.CancelFunc
	shutdown []func(context.Context) error
}

// Setup installs the global tracer, meter and logger providers, makes slog
// the default logger feeding both stdout and the kept logs, and starts
// sampling metrics.
func Setup(ctx context.Context, opts Options) (*Telemetry, error) {
	version, revision := BuildVersion(opts.Version)
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(opts.ServiceName),
		semconv.ServiceVersion(version),
	))
	if err != nil {
		return nil, err
	}

	hostname, _ := os.Hostname()
	t := &Telemetry{
		Run: Event{
			Time: time.Now(), Kind: EventStart, Version: version, Revision: revision,
			GoVersion: goruntime.Version(), Host: hostname, PID: os.Getpid(),
		},
		stopReason: "shut down",
		Started:    time.Now(),
		Spans:      newSpanStore(opts.Spans),
		Logs:       newLogStore(opts.Logs),
		Exporting:  otlpConfigured(),

		SampleEvery: opts.SampleEvery,
		Window:      opts.Window,
	}

	tracerOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(t.Spans)),
	}
	reader := sdkmetric.NewManualReader()
	t.Metrics = newMetricStore(reader, opts.Window, opts.SampleEvery)
	meterOpts := []sdkmetric.Option{sdkmetric.WithResource(res), sdkmetric.WithReader(reader)}
	loggerOpts := []sdklog.LoggerProviderOption{
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewSimpleProcessor(t.Logs)),
	}

	if t.Exporting {
		traces, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, err
		}
		metrics, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return nil, err
		}
		logs, err := otlploghttp.New(ctx)
		if err != nil {
			return nil, err
		}
		tracerOpts = append(tracerOpts, sdktrace.WithBatcher(traces))
		meterOpts = append(meterOpts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metrics)))
		loggerOpts = append(loggerOpts, sdklog.WithProcessor(sdklog.NewBatchProcessor(logs)))
	}

	tracers := sdktrace.NewTracerProvider(tracerOpts...)
	meters := sdkmetric.NewMeterProvider(meterOpts...)
	loggers := sdklog.NewLoggerProvider(loggerOpts...)
	t.shutdown = append(t.shutdown, tracers.Shutdown, meters.Shutdown, loggers.Shutdown)

	otel.SetTracerProvider(tracers)
	otel.SetMeterProvider(meters)
	global.SetLoggerProvider(loggers)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	slog.SetDefault(slog.New(fanout{
		slog.NewTextHandler(opts.Stdout, nil),
		otelslog.NewHandler(opts.ServiceName, otelslog.WithLoggerProvider(loggers)),
	}))

	if opts.Runtime {
		if err := runtime.Start(runtime.WithMeterProvider(meters)); err != nil {
			return nil, err
		}
		if err := host.Start(host.WithMeterProvider(meters)); err != nil {
			return nil, err
		}
	}

	// Opened once slog is set up, so a file another process holds is
	// reported. Telemetry carries on in memory without it.
	if opts.Path != "" && opts.Retention > 0 {
		d, err := openDisk(opts.Path, opts.Retention)
		if err != nil {
			slog.Warn("telemetry is kept in memory only: could not open its file", "path", opts.Path, "error", err)
		} else {
			t.disk, t.Retention, t.Path, t.PersistEvery = d, opts.Retention, opts.Path, opts.PersistEvery
			t.Metrics.disk, t.Logs.disk, t.Spans.disk = d, d, d
			if err := d.mark(t.Run); err != nil {
				slog.Warn("could not mark the start of this run", "error", err)
			}
		}
	}
	slog.Info("budge started", "version", version, "revision", revision, "pid", t.Run.PID)

	sampling, stop := context.WithCancel(context.Background())
	t.stop = stop
	t.sampling = make(chan struct{})
	go t.sample(sampling, opts.SampleEvery, opts.PersistEvery)
	return t, nil
}

// pruneEvery is how often what has outlived the retention is dropped.
const pruneEvery = time.Hour

// sample collects metrics on an interval until stopped, writing them and the
// logs to disk as often as asked, and pruning what is past keeping.
func (t *Telemetry) sample(ctx context.Context, every, persistEvery time.Duration) {
	defer close(t.sampling)
	tick := time.NewTicker(every)
	defer tick.Stop()
	var persisted, pruned time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			if err := t.Metrics.Collect(ctx); err != nil {
				slog.Warn("could not sample metrics", "error", err)
			}
			if t.disk == nil {
				continue
			}
			if now.Sub(persisted) >= persistEvery {
				t.persist()
				persisted = now
			}
			if now.Sub(pruned) >= pruneEvery {
				if err := t.disk.prune(now); err != nil {
					slog.Warn("could not prune telemetry", "error", err)
				}
				pruned = now
				// Pruning frees pages bolt keeps for itself; once enough of
				// the file is free, it is packed down to give them back.
				if t.disk.worthCompacting() {
					_, _ = t.Compact()
				}
			}
		}
	}
}

// persist writes the newest samples and the logs since the last write to
// disk. Logs a write failed to keep are tried again next time.
func (t *Telemetry) persist() {
	samples, records, spans := t.Metrics.unpersisted(), t.Logs.takePending(), t.Spans.takePending()
	if err := t.disk.write(samples, records, spans); err != nil {
		t.Logs.requeue(records)
		t.Spans.requeue(spans)
		slog.Warn("could not write telemetry to disk", "error", err)
		return
	}
	t.Metrics.markPersisted(samples)
}

// Compact packs the telemetry file down, giving back the room pruning made.
func (t *Telemetry) Compact() (Compaction, error) {
	if t.disk == nil {
		return Compaction{}, errors.New("telemetry is kept in memory only")
	}
	result, err := t.disk.compact()
	if err != nil {
		slog.Error("could not compact telemetry", "error", err)
		return result, err
	}
	slog.Info("compacted telemetry", "before", result.Before, "after", result.After, "took", result.Took)
	return result, nil
}

// Uptime is how long the process has been running.
func (t *Telemetry) Uptime() time.Duration { return time.Since(t.Started) }

// Shutdown stops sampling, flushes anything still to be exported, and
// writes what is waiting to disk.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	t.stop()
	<-t.sampling
	var errs []error
	for _, shutdown := range t.shutdown {
		errs = append(errs, shutdown(ctx))
	}
	if t.disk != nil {
		samples, records, spans := t.Metrics.unpersisted(), t.Logs.takePending(), t.Spans.takePending()
		stop := t.Run
		stop.Time, stop.Kind, stop.Reason = time.Now(), EventStop, t.stopReason
		errs = append(errs, t.disk.write(samples, records, spans), t.disk.mark(stop), t.disk.close())
	}
	return errors.Join(errs...)
}

// StoppingBecause notes why budge is stopping, for the mark Shutdown leaves.
func (t *Telemetry) StoppingBecause(reason string) {
	t.stopReason = reason
	slog.Info("budge stopping", "reason", reason)
}

// Run is one run of budge, from its start to its stop. A run that never
// marked its stop ended without shutting down: it crashed, was killed, or
// the machine went away.
type Run struct {
	Start   Event
	Stop    Event
	Stopped bool
	Current bool
}

// Ended reports a run that is over without having shut down.
func (r Run) Ended() bool { return !r.Stopped && !r.Current }

// Runs lists the runs kept on disk, newest first. Without a file there is
// only this one.
func (t *Telemetry) Runs() []Run {
	if t.disk == nil {
		return []Run{{Start: t.Run, Current: true}}
	}
	events, err := t.disk.events()
	if err != nil {
		slog.Warn("could not read the runs", "error", err)
	}
	var runs []Run
	for _, event := range events {
		switch event.Kind {
		case EventStart:
			runs = append(runs, Run{Start: event})
		case EventStop:
			// A stop belongs to the start of the same process.
			if n := len(runs); n > 0 && !runs[n-1].Stopped && runs[n-1].Start.PID == event.PID && runs[n-1].Start.Host == event.Host {
				runs[n-1].Stop, runs[n-1].Stopped = event, true
			}
		}
	}
	for i := range runs {
		runs[i].Current = runs[i].Start.Time.Equal(t.Run.Time) && runs[i].Start.PID == t.Run.PID
	}
	slices.Reverse(runs)
	return runs
}

// BuildVersion is the version budge was built as: the one given, stamped in
// at release, or else what Go recorded from the module and its checkout.
func BuildVersion(given string) (version, revision string) {
	version = given
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return cmp.Or(version, "dev"), ""
	}
	if version == "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	var dirty bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value[:min(len(setting.Value), 7)]
		case "vcs.modified":
			dirty = setting.Value == "true"
		}
	}
	if dirty && revision != "" {
		revision += "-dirty"
	}
	return cmp.Or(version, "dev"), revision
}

// Storage is how much telemetry is kept, and where.
type Storage struct {
	Retention time.Duration
	Path      string
	FileBytes int64
	Series    int
	Points    int
	Logs      int
	Spans     int

	// Reclaimable is free space in the file a compaction would give back,
	// and Compacted the last time one did.
	Reclaimable int64
	Compacted   Compaction

	Window       time.Duration
	MemorySpans  int
	MemoryLogs   int
	MemorySeries int
	MemoryPoints int
}

// Storage counts what is kept in memory and on disk.
func (t *Telemetry) Storage() Storage {
	s := Storage{
		Retention:   t.Retention,
		Path:        t.Path,
		Window:      t.Window,
		MemorySpans: t.Spans.roots.len() + t.Spans.children.len(),
		MemoryLogs:  t.Logs.records.len(),
	}
	s.MemorySeries, s.MemoryPoints = t.Metrics.counts()
	if t.disk != nil {
		if err := t.disk.stats(&s); err != nil {
			slog.Warn("could not measure the telemetry file", "error", err)
		}
	}
	return s
}

// otlpConfigured reports whether any of the standard OTLP endpoint variables
// are set, which is what turns exporting on.
func otlpConfigured() bool {
	for _, name := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
	} {
		if os.Getenv(name) != "" {
			return true
		}
	}
	return false
}

// fanout hands every log record to each of its handlers.
type fanout []slog.Handler

func (f fanout) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanout) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, h := range f {
		if h.Enabled(ctx, record.Level) {
			errs = append(errs, h.Handle(ctx, record.Clone()))
		}
	}
	return errors.Join(errs...)
}

func (f fanout) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (f fanout) WithGroup(name string) slog.Handler {
	out := make(fanout, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(name)
	}
	return out
}

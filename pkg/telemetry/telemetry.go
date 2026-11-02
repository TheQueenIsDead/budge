// Package telemetry instruments budge with OpenTelemetry, and keeps what it
// records in memory for the admin page: the recent spans, logs and metrics
// since the process started. When the standard OTLP environment variables
// are set, the same data is also exported to a collector.
package telemetry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
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
}

// DefaultOptions keeps a day of metrics sampled every 15 seconds, and the
// last few thousand spans and logs.
func DefaultOptions() Options {
	return Options{
		ServiceName: "budge",
		Spans:       5000,
		Logs:        5000,
		SampleEvery: 15 * time.Second,
		Window:      24 * time.Hour,
		Stdout:      os.Stdout,
		Runtime:     true,
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

	// SampleEvery is how often metrics are sampled.
	SampleEvery time.Duration

	stop     context.CancelFunc
	shutdown []func(context.Context) error
}

// Setup installs the global tracer, meter and logger providers, makes slog
// the default logger feeding both stdout and the kept logs, and starts
// sampling metrics.
func Setup(ctx context.Context, opts Options) (*Telemetry, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(opts.ServiceName),
		semconv.ServiceVersion(opts.Version),
	))
	if err != nil {
		return nil, err
	}

	t := &Telemetry{
		Started:   time.Now(),
		Spans:     newSpanStore(opts.Spans),
		Logs:      newLogStore(opts.Logs),
		Exporting: otlpConfigured(),

		SampleEvery: opts.SampleEvery,
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

	sampling, stop := context.WithCancel(context.Background())
	t.stop = stop
	go t.sample(sampling, opts.SampleEvery)
	return t, nil
}

// sample collects metrics on an interval until stopped.
func (t *Telemetry) sample(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := t.Metrics.Collect(ctx); err != nil {
				slog.Warn("could not sample metrics", "error", err)
			}
		}
	}
}

// Uptime is how long the process has been running.
func (t *Telemetry) Uptime() time.Duration { return time.Since(t.Started) }

// Shutdown stops sampling and flushes anything still to be exported.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	t.stop()
	var errs []error
	for _, shutdown := range t.shutdown {
		errs = append(errs, shutdown(ctx))
	}
	return errors.Join(errs...)
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

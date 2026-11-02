package database

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	bolt "go.etcd.io/bbolt"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	module      = "github.com/TheQueenIsDead/budge/pkg/"
	thisPackage = module + "database."
)

// instruments are the tracer and duration histogram from the providers
// installed when they were made. Telemetry may be set up again, as each test
// does, so they are remade whenever the providers change rather than held
// from the first.
type instruments struct {
	tracers   trace.TracerProvider
	meters    metric.MeterProvider
	tracer    trace.Tracer
	durations metric.Float64Histogram
}

var current atomic.Pointer[instruments]

func instrumented() *instruments {
	tracers, meters := otel.GetTracerProvider(), otel.GetMeterProvider()
	if in := current.Load(); in != nil && in.tracers == tracers && in.meters == meters {
		return in
	}
	// Bolt answers most reads in well under a millisecond, so the buckets
	// start far finer than the SDK's defaults.
	durations, _ := meters.Meter(module+"database").Float64Histogram(
		"db.client.operation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("How long each bolt transaction took"),
		metric.WithExplicitBucketBoundaries(0.00005, 0.0001, 0.00025, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1),
	)
	in := &instruments{tracers: tracers, meters: meters, tracer: tracers.Tracer(module + "database"), durations: durations}
	current.Store(in)
	return in
}

// view runs a read-only transaction, traced as a child of ctx.
func view(ctx context.Context, db *bolt.DB, bucket string, fn func(*bolt.Tx) error) error {
	return traced(ctx, "view", bucket, func() error { return db.View(fn) })
}

// update runs a read-write transaction, traced as a child of ctx.
func update(ctx context.Context, db *bolt.DB, bucket string, fn func(*bolt.Tx) error) error {
	return traced(ctx, "update", bucket, func() error { return db.Update(fn) })
}

// traced times a transaction and records it as a span and a duration,
// naming the Store method that ran it and the code outside the database
// package that asked for it.
func traced(ctx context.Context, kind, bucket string, run func() error) error {
	operation, caller := origin()
	attrs := []attribute.KeyValue{
		attribute.String("db.system.name", "bbolt"),
		attribute.String("db.operation.name", operation),
		attribute.String("db.collection.name", bucket),
		attribute.String("bbolt.transaction", kind),
		attribute.String("code.function.name", caller.Function),
	}

	in := instrumented()
	ctx, span := in.tracer.Start(ctx, operation+" "+bucket,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
		trace.WithAttributes(
			attribute.String("code.file.path", caller.File),
			attribute.Int("code.line.number", caller.Line),
		),
	)
	start := time.Now()
	err := run()
	in.durations.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
	return err
}

// origin walks the stack to find the Store method running the transaction,
// which names the operation, and the first frame beyond the database package,
// which is where the query came from. Tests within this package count as
// beyond it.
func origin() (operation string, caller runtime.Frame) {
	pcs := make([]uintptr, 32)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)])
	var method bool
	for {
		frame, more := frames.Next()
		inside := strings.HasPrefix(frame.Function, thisPackage) && !strings.HasSuffix(frame.File, "_test.go")
		if !inside {
			frame.Function = shortName(frame.Function)
			return operation, frame
		}
		if !method {
			operation = operationName(frame.Function)
			method = strings.HasPrefix(frame.Function, thisPackage+"(*Store).")
		}
		if !more {
			return operation, runtime.Frame{}
		}
	}
}

// operationName names a database function by itself, without its package,
// receiver, type parameters or closures: "ReadTransactions" rather than
// "database.(*Store).ReadTransactions.func1".
func operationName(function string) string {
	name := strings.TrimPrefix(function, thisPackage)
	name = strings.TrimPrefix(name, "(*Store).")
	if i := strings.IndexAny(name, ".["); i >= 0 {
		name = name[:i]
	}
	return name
}

// shortName drops the module path from a function name, leaving the package:
// "application.(*Application).Budget".
func shortName(function string) string {
	return strings.TrimPrefix(function, module)
}

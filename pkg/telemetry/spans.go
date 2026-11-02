package telemetry

import (
	"context"
	"sort"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Span is a finished span as the admin page shows it.
type Span struct {
	TraceID  string
	SpanID   string
	ParentID string
	Name     string
	Kind     string
	Start    time.Time
	Duration time.Duration
	Error    bool
	Status   string
	Attrs    map[string]string
}

// SpanStore keeps the most recent finished spans. It is a span exporter, so
// the SDK hands it every span as it ends.
type SpanStore struct {
	spans *ring[Span]
}

func newSpanStore(capacity int) *SpanStore {
	return &SpanStore{spans: newRing[Span](capacity)}
}

// ExportSpans keeps the spans handed to it.
func (s *SpanStore) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	kept := make([]Span, 0, len(spans))
	for _, span := range spans {
		attrs := make(map[string]string, len(span.Attributes()))
		for _, kv := range span.Attributes() {
			attrs[string(kv.Key)] = kv.Value.Emit()
		}
		var parent string
		if span.Parent().IsValid() {
			parent = span.Parent().SpanID().String()
		}
		kept = append(kept, Span{
			TraceID:  span.SpanContext().TraceID().String(),
			SpanID:   span.SpanContext().SpanID().String(),
			ParentID: parent,
			Name:     span.Name(),
			Kind:     span.SpanKind().String(),
			Start:    span.StartTime(),
			Duration: span.EndTime().Sub(span.StartTime()),
			Error:    span.Status().Code == codes.Error,
			Status:   span.Status().Description,
			Attrs:    attrs,
		})
	}
	s.spans.add(kept...)
	return nil
}

// Shutdown has nothing to release.
func (s *SpanStore) Shutdown(context.Context) error { return nil }

// Recent returns the kept spans, newest first, that match a filter.
func (s *SpanStore) Recent(match func(Span) bool) []Span {
	var out []Span
	for _, span := range s.spans.all() {
		if match == nil || match(span) {
			out = append(out, span)
		}
	}
	return out
}

// Requests returns the kept server spans, one for each request handled,
// newest first.
func (s *SpanStore) Requests() []Span {
	server := trace.SpanKindServer.String()
	return s.Recent(func(span Span) bool { return span.Kind == server })
}

// Trace returns a trace's kept spans, earliest first, so a request and
// everything it did read as a waterfall.
func (s *SpanStore) Trace(traceID string) []Span {
	spans := s.Recent(func(span Span) bool { return span.TraceID == traceID })
	sort.Slice(spans, func(i, j int) bool { return spans[i].Start.Before(spans[j].Start) })
	return spans
}

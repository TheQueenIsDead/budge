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

// End is when the span finished.
func (s Span) End() time.Time { return s.Start.Add(s.Duration) }

// SpanStore keeps the most recent finished spans. It is a span exporter, so
// the SDK hands it every span as it ends.
//
// A request makes many queries, so roots and the spans beneath them are kept
// apart: a page that reads the store twenty times does not push nineteen
// older requests out of view.
type SpanStore struct {
	roots    *ring[Span]
	children *ring[Span]
}

// childrenPerRoot is how many spans beneath a root are kept for each root.
const childrenPerRoot = 4

func newSpanStore(capacity int) *SpanStore {
	return &SpanStore{roots: newRing[Span](capacity), children: newRing[Span](capacity * childrenPerRoot)}
}

// ExportSpans keeps the spans handed to it.
func (s *SpanStore) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	var roots, children []Span
	for _, span := range spans {
		attrs := make(map[string]string, len(span.Attributes()))
		for _, kv := range span.Attributes() {
			attrs[string(kv.Key)] = kv.Value.Emit()
		}
		var parent string
		if span.Parent().IsValid() {
			parent = span.Parent().SpanID().String()
		}
		kept := Span{
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
		}
		if parent == "" {
			roots = append(roots, kept)
		} else {
			children = append(children, kept)
		}
	}
	s.roots.add(roots...)
	s.children.add(children...)
	return nil
}

// Shutdown has nothing to release.
func (s *SpanStore) Shutdown(context.Context) error { return nil }

// Recent returns the kept spans that match a filter, the most recently
// finished first.
func (s *SpanStore) Recent(match func(Span) bool) []Span {
	out := append(filter(s.roots.all(), match), filter(s.children.all(), match)...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].End().After(out[j].End()) })
	return out
}

// Roots returns the kept spans that began a trace, newest first: requests,
// and the jobs budge runs by itself.
func (s *SpanStore) Roots() []Span {
	return s.roots.all()
}

// Requests returns the kept server spans, one for each request handled,
// newest first.
func (s *SpanStore) Requests() []Span {
	server := trace.SpanKindServer.String()
	return filter(s.roots.all(), func(span Span) bool { return span.Kind == server })
}

func filter(spans []Span, match func(Span) bool) []Span {
	if match == nil {
		return spans
	}
	var out []Span
	for _, span := range spans {
		if match(span) {
			out = append(out, span)
		}
	}
	return out
}

// Trace returns a trace's kept spans, earliest first, so a request and
// everything it did read as a waterfall.
func (s *SpanStore) Trace(traceID string) []Span {
	spans := s.Recent(func(span Span) bool { return span.TraceID == traceID })
	sort.Slice(spans, func(i, j int) bool { return spans[i].Start.Before(spans[j].Start) })
	return spans
}

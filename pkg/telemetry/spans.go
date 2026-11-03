package telemetry

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
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
	Events   []SpanEvent `json:",omitempty"`
}

// SpanEvent is something that happened during a span: most often an
// exception, recorded with its type, message and, when known, its stack.
type SpanEvent struct {
	Name  string
	Time  time.Time
	Attrs map[string]string
}

// Exception is an exception event, read for showing.
type Exception struct {
	Type    string
	Message string
	Stack   string
}

// shortType names a Go type by its package's last element, as Go prints it:
// "*application.recovered" rather than its whole import path.
func shortType(name string) string {
	pointer := strings.HasPrefix(name, "*")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
		if pointer {
			name = "*" + name
		}
	}
	return name
}

// Exceptions are the exceptions recorded on the span, in order.
func (s Span) Exceptions() []Exception {
	var out []Exception
	for _, event := range s.Events {
		if event.Name != "exception" {
			continue
		}
		out = append(out, Exception{
			Type:    shortType(event.Attrs["exception.type"]),
			Message: event.Attrs["exception.message"],
			Stack:   event.Attrs["exception.stacktrace"],
		})
	}
	return out
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
	disk     *disk

	// pending is what has finished since the last write to disk.
	mu      sync.Mutex
	pending []Span
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
			attrs[string(kv.Key)] = kv.Value.String()
		}
		var parent string
		if span.Parent().IsValid() {
			parent = span.Parent().SpanID().String()
		}
		var events []SpanEvent
		for _, event := range span.Events() {
			eventAttrs := make(map[string]string, len(event.Attributes))
			for _, kv := range event.Attributes {
				eventAttrs[string(kv.Key)] = kv.Value.String()
			}
			events = append(events, SpanEvent{Name: event.Name, Time: event.Time, Attrs: eventAttrs})
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
			Events:   events,
		}
		if parent == "" {
			roots = append(roots, kept)
		} else {
			children = append(children, kept)
		}
	}
	s.roots.add(roots...)
	s.children.add(children...)
	if s.disk != nil {
		s.mu.Lock()
		s.pending = append(append(s.pending, roots...), children...)
		s.mu.Unlock()
	}
	return nil
}

// takePending hands over what is waiting to be written to disk.
func (s *SpanStore) takePending() []Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending
	s.pending = nil
	return pending
}

// requeue puts back spans a write to disk failed to keep.
func (s *SpanStore) requeue(spans []Span) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(spans, s.pending...)
}

// History walks the roots kept, newest first, through memory and then the
// disk from before the oldest in memory, until visit returns false: every
// request and job across the retention.
func (s *SpanStore) History(visit func(Span) bool) {
	recent := s.roots.all()
	for _, span := range recent {
		if !visit(span) {
			return
		}
	}
	if s.disk == nil {
		return
	}
	// The disk is keyed by when a root ended, and what is in memory has
	// mostly been written there too.
	before := time.Now().Add(time.Hour)
	if len(recent) > 0 {
		before = recent[len(recent)-1].End()
	}
	if err := s.disk.roots(before, visit); err != nil {
		slog.Warn("could not read traces from disk", "error", err)
	}
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
	// A trace's spans may since have left memory, in part or whole.
	if s.disk != nil {
		kept, err := s.disk.trace(traceID)
		if err != nil {
			slog.Warn("could not read a trace from disk", "trace", traceID, "error", err)
		}
		seen := map[string]bool{}
		for _, span := range spans {
			seen[span.SpanID] = true
		}
		for _, span := range kept {
			if !seen[span.SpanID] {
				spans = append(spans, span)
			}
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].Start.Before(spans[j].Start) })
	return spans
}

package telemetry

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// LogRecord is a log entry as the admin page shows it.
type LogRecord struct {
	Time     time.Time
	Severity log.Severity
	Level    string
	Message  string
	Attrs    map[string]string
	TraceID  string
}

// IsError reports whether the record is an error or worse.
func (r LogRecord) IsError() bool { return r.Severity >= log.SeverityError }

// LogStore keeps the most recent log records. It is a log exporter, so the
// SDK hands it every record as it is logged.
type LogStore struct {
	records *ring[LogRecord]
	disk    *disk

	// pending is what has been logged since the last write to disk.
	mu      sync.Mutex
	pending []LogRecord
}

func newLogStore(capacity int) *LogStore {
	return &LogStore{records: newRing[LogRecord](capacity)}
}

// Export keeps the records handed to it.
func (s *LogStore) Export(_ context.Context, records []sdklog.Record) error {
	kept := make([]LogRecord, 0, len(records))
	for _, record := range records {
		attrs := map[string]string{}
		record.WalkAttributes(func(kv attribute.KeyValue) bool {
			attrs[string(kv.Key)] = kv.Value.Emit()
			return true
		})
		var traceID string
		if record.TraceID().IsValid() {
			traceID = record.TraceID().String()
		}
		at := record.Timestamp()
		if at.IsZero() {
			at = record.ObservedTimestamp()
		}
		kept = append(kept, LogRecord{
			Time:     at,
			Severity: record.Severity(),
			Level:    levelName(record.Severity()),
			Message:  record.Body().String(),
			Attrs:    attrs,
			TraceID:  traceID,
		})
	}
	s.records.add(kept...)
	if s.disk != nil {
		s.mu.Lock()
		s.pending = append(s.pending, kept...)
		s.mu.Unlock()
	}
	return nil
}

// takePending hands over what is waiting to be written to disk.
func (s *LogStore) takePending() []LogRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending
	s.pending = nil
	return pending
}

// requeue puts back records a write to disk failed to keep, ahead of any
// logged since, so the next write tries them again.
func (s *LogStore) requeue(records []LogRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(records, s.pending...)
}

// Search finds records at or above a severity that match, newest first,
// across the whole retention: those in memory, then those on disk from
// before them. It returns up to limit of them, and how many match in all.
func (s *LogStore) Search(min log.Severity, match func(LogRecord) bool, limit int) ([]LogRecord, int) {
	var out []LogRecord
	total := 0
	visit := func(record LogRecord) bool {
		if record.Severity < min || (match != nil && !match(record)) {
			return true
		}
		total++
		if len(out) < limit {
			out = append(out, record)
		}
		return true
	}
	recent := s.records.all()
	for _, record := range recent {
		visit(record)
	}
	if s.disk == nil {
		return out, total
	}
	// What is in memory has mostly been written to disk too, so the disk is
	// read only from before the oldest record still in memory.
	before := time.Now().Add(time.Hour)
	if len(recent) > 0 {
		before = recent[len(recent)-1].Time
	}
	if err := s.disk.logs(before, visit); err != nil {
		slog.Warn("could not read logs from disk", "error", err)
	}
	return out, total
}

// Shutdown has nothing to release.
func (s *LogStore) Shutdown(context.Context) error { return nil }

// ForceFlush has nothing to flush: records are kept as they arrive.
func (s *LogStore) ForceFlush(context.Context) error { return nil }

// Recent returns the kept records at or above a severity, newest first.
func (s *LogStore) Recent(min log.Severity) []LogRecord {
	var out []LogRecord
	for _, record := range s.records.all() {
		if record.Severity >= min {
			out = append(out, record)
		}
	}
	return out
}

// levelName names a severity the way slog does.
func levelName(severity log.Severity) string {
	switch {
	case severity >= log.SeverityError:
		return "ERROR"
	case severity >= log.SeverityWarn:
		return "WARN"
	case severity >= log.SeverityInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

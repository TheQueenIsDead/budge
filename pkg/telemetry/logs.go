package telemetry

import (
	"context"
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
	return nil
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

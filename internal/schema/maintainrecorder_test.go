package schema

import (
	"context"
	"log/slog"
	"slices"
	"sync"
)

// This file is the reader both tiers assert a pass's log lines through. It carries **no build tag**
// on purpose, for the reason harness_test.go carries none: the container-backed cases of Steps 11
// and 14 read a pass that reached the catalog, and the container-free ones read a pass that refused
// its configuration before it reached anything -- and a second recorder for the second tier would
// drift the moment one of them learned about a field. It arrived here from
// maintain_integration_test.go when the horizon refusal gave the untagged tier its first log line.

// loggedRecord is one record as these cases read it: the level an alerting rule keys on, the
// message, and the attributes by name.
type loggedRecord struct {
	level   slog.Level
	message string
	attrs   map[string]string
}

// logRecorder keeps every record a pass wrote. It is a handler rather than a buffer to parse back,
// because what the cases assert is the record -- its level and its named attributes -- and a text
// rendering would make them assert slog's formatting instead.
type logRecorder struct {
	mu      sync.Mutex
	records []loggedRecord
}

func (recorder *logRecorder) Enabled(context.Context, slog.Level) bool { return true }

func (recorder *logRecorder) Handle(_ context.Context, record slog.Record) error {
	written := loggedRecord{level: record.Level, message: record.Message, attrs: map[string]string{}}
	record.Attrs(func(attr slog.Attr) bool {
		written.attrs[attr.Key] = attr.Value.String()
		return true
	})

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.records = append(recorder.records, written)
	return nil
}

func (recorder *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return recorder }
func (recorder *logRecorder) WithGroup(string) slog.Handler      { return recorder }

// taken is the records written so far, copied under the lock because the three-replica precursor
// writes into one recorder from three goroutines.
func (recorder *logRecorder) taken() []loggedRecord {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return slices.Clone(recorder.records)
}

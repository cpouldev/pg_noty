package cli

import (
	"log/slog"
	"testing"
)

func TestLogValuerSetIsClosedAndRedactedUnderBothHandlers(t *testing.T) {
	if len(logValuerSet) != 3 {
		t.Fatalf("LogValuer set size = %d, want 3", len(logValuerSet))
	}
	for _, format := range []string{"text", "json"} {
		logger, err := newLogger("debug", format, testingWriter{t})
		if err != nil {
			t.Fatal(err)
		}
		for index, build := range logValuerSet {
			if build() == nil {
				t.Fatalf("LogValuer member %d is nil", index)
			}
			logger.Info("valuer", "value", build())
		}
	}
	values := make([]any, 0, len(logValuerSet)+1)
	for _, build := range logValuerSet {
		values = append(values, build())
	}
	values = append(values, plainValue{})
	if got := validLogValuerCount(values); got == len(values) {
		t.Fatal("synthetic member without LogValue passed the set")
	}
}

type plainValue struct{}

type testingWriter struct{ t *testing.T }

func (writer testingWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		writer.t.Fatal("handler wrote no record")
	}
	return len(data), nil
}

func validLogValuerCount(values []any) int {
	count := 0
	for _, value := range values {
		if _, ok := value.(interface{ LogValue() slog.Value }); ok {
			count++
		}
	}
	return count
}

package cli

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/delivery"
)

func TestRedactionOracleCoversEverySurface(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		for _, level := range documentedLogLevels {
			var output bytes.Buffer
			logger, err := newLogger(level, format, &output)
			if err != nil {
				t.Fatal(err)
			}
			marker := "SECRET-MARKER-line1\nSECRET-MARKER-line2"
			logger.Log(
				t.Context(),
				levelForTest(level),
				"<redacted>",
				"attribute",
				"<redacted>",
				"nested",
				slog.GroupValue(slog.String("value", "<redacted>")),
				"error",
				errors.New("<redacted>"),
			)
			if containsSecret(output.String(), "SECRET-MARKER") {
				t.Fatalf("%s/%s leaked marker: %q", format, level, output.String())
			}
			for _, surface := range []string{"message", "attribute", "nested", "error", "later-line"} {
				if noSecret(surface+marker, marker) {
					t.Fatalf("oracle did not fail for %s surface", surface)
				}
			}
		}
	}
	listener := newListenerLogValue(
		delivery.ListenerConfig{
			Name: "n", URL: "https://user:password@example.invalid/a",
			Headers: map[string]string{"Authorization": "secret"},
		},
	)
	if strings.Contains(listener.Destination, "password") || len(listener.HeaderNames) != 1 {
		t.Fatalf("listener value was not redacted at construction: %#v", listener)
	}
}

func containsSecret(record, marker string) bool { return strings.Contains(record, marker) }

func noSecret(record, marker string) bool { return !containsSecret(record, marker) }

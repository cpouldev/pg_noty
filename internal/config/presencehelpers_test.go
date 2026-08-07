package config

import (
	"strconv"
	"testing"
	"time"
)

// listenerReporting is the variant that retains the diagnostic for an unreadable value.
func listenerReporting(t *testing.T, retryLines string) (rawListener, Errors) {
	t.Helper()

	retry := ""
	if retryLines != "" {
		retry = "    retry:\n" + retryLines
	}
	document := aListenerOf(requiredName, requiredTable, requiredOperations, requiredDestination, retry)

	decoded, diags := stageG(t, document)
	if len(decoded.Listeners.Values) != 1 {
		t.Fatalf("the document decoded %d listeners, want 1:\n%s",
			len(decoded.Listeners.Values), document)
	}
	return decoded.Listeners.Values[0], diags
}

// oneDecodedListener requires exactly one listener and no earlier-stage refusal.
func oneDecodedListener(t *testing.T, document string) rawListener {
	t.Helper()

	raw := decodedConfig(t, document)
	if len(raw.Listeners.Values) != 1 {
		t.Fatalf("the document decoded %d listeners, want 1:\n%s",
			len(raw.Listeners.Values), document)
	}
	return raw.Listeners.Values[0]
}

func boolText(held bool) string { return strconv.FormatBool(held) }

func intText(held int) string { return strconv.Itoa(held) }

func durText(held time.Duration) string { return held.String() }

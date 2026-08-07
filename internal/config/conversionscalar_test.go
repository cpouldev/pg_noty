package config

import (
	"fmt"
	"testing"
	"time"

	"github.com/goccy/go-yaml/ast"
)

// TestAnIntegerIsCountedAndNothingElse pins conversion from both sides. A float is the case
// mutation testing found: reading 1.5 through ParseFloat and truncating it would invent 1.
func TestAnIntegerIsCountedAndNothingElse(t *testing.T) {
	accepted := map[string]int{"16": 16, "0x10": 16, "0o20": 16, "-3": -3, "0": 0,
		`"16"`: 16, "16.0": 16}
	refused := []string{"1.5", "1e3", `"1_000"`, "true", `""`, `" 16"`, "16abc",
		"99999999999999999999"}

	for written, want := range accepted {
		t.Run("accepts "+written, func(t *testing.T) {
			got := kindNamed(t, "Int").read(nodeReadBy(t, "value: "+written+"\n", "$.value"))
			if len(got.diags) != 0 {
				t.Fatalf("recorded %q for an integer", messagesOf(got.diags))
			}
			if got.value != fmt.Sprint(want) {
				t.Errorf("read %s, want %d", got.value, want)
			}
		})
	}
	for _, written := range refused {
		t.Run("refuses "+written, func(t *testing.T) {
			got := kindNamed(t, "Int").read(nodeReadBy(t, "value: "+written+"\n", "$.value"))
			if len(got.diags) != 1 || got.diags[0].Msg != mustBeAnInteger.message {
				t.Errorf("recorded %q, want one refusal naming the expected type", messagesOf(got.diags))
			}
		})
	}
}

// TestABooleanIsTrueOrFalseAndNothingElse rejects ParseBool's non-YAML boolean shortcuts.
func TestABooleanIsTrueOrFalseAndNothingElse(t *testing.T) {
	accepted := map[string]bool{"true": true, "True": true, "TRUE": true, `"true"`: true,
		"false": false, "FALSE": false, `"False"`: false}
	refused := []string{"1", "0", "t", "f", "yes", "no", "on", "off", `""`, "truthy"}

	for written, want := range accepted {
		t.Run("accepts "+written, func(t *testing.T) {
			got := kindNamed(t, "Bool").read(nodeReadBy(t, "value: "+written+"\n", "$.value"))
			if len(got.diags) != 0 {
				t.Fatalf("recorded %q for a boolean", messagesOf(got.diags))
			}
			if got.value != fmt.Sprint(want) {
				t.Errorf("read %s, want %t", got.value, want)
			}
		})
	}
	for _, written := range refused {
		t.Run("refuses "+written, func(t *testing.T) {
			got := kindNamed(t, "Bool").read(nodeReadBy(t, "value: "+written+"\n", "$.value"))
			if len(got.diags) != 1 || got.diags[0].Msg != mustBeABoolean.message {
				t.Errorf("recorded %q, want one refusal naming the expected type", messagesOf(got.diags))
			}
		})
	}
}

// TestADurationAcceptsTheGoUnitsAndNothingElse pins the accepted-unit set.
func TestADurationAcceptsTheGoUnitsAndNothingElse(t *testing.T) {
	accepted := map[string]time.Duration{
		"500ns": 500 * time.Nanosecond, "500us": 500 * time.Microsecond,
		"500µs": 500 * time.Microsecond, "500ms": 500 * time.Millisecond,
		"30s": 30 * time.Second, "5m": 5 * time.Minute, "168h": 168 * time.Hour,
		"1h30m": 90 * time.Minute, "-1s": -time.Second,
	}
	refused := []string{"7d", "1w", "30", "", "10 s", "abc"}

	for written, want := range accepted {
		t.Run("accepts "+written, func(t *testing.T) {
			var held Dur
			src, node := quotedScalar(t, written)
			got := readInto(t, src, &held, node, func() string { return fmt.Sprint(held.value) })
			if len(got.diags) != 0 {
				t.Fatalf("recorded %q for a duration in Go units", messagesOf(got.diags))
			}
			if held.value != want {
				t.Errorf("read %v, want %v", held.value, want)
			}
		})
	}
	for _, written := range refused {
		t.Run("refuses "+written, func(t *testing.T) {
			var held Dur
			src, node := quotedScalar(t, written)
			got := readInto(t, src, &held, node, func() string { return fmt.Sprint(held.value) })
			if len(got.diags) != 1 {
				t.Errorf("recorded %d diagnostics %q, want one refusing a duration outside the Go units",
					len(got.diags), messagesOf(got.diags))
			}
		})
	}
}

func quotedScalar(t *testing.T, written string) (*source, ast.Node) {
	t.Helper()
	return nodeAt(t, "value: "+fmt.Sprintf("%q", written)+"\n", "$.value")
}

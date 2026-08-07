package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestMarkerUsesTheLongOperationSpellingAndSpecificationForm(t *testing.T) {
	if got := marker("demo", "order_paid", "update"); got != "pg_noty:v1:demo:order_paid:update" {
		t.Fatalf("marker = %q, want pg_noty:v1:demo:order_paid:update", got)
	}
}

// theLongestOperationSpelling is the widest member of the closed operation vocabulary in its long
// form, so the marker arithmetic below is derived from the vocabulary rather than transcribed.
const theLongestOperationSpelling = "update"

// TestMarkerHasNoIdentifierLengthBound walks the marker's documented ceiling and both of its
// adjacents. 101 is the longest marker a valid configuration admits -- 11 bytes of prefix,
// internal/config's 41-byte instance maximum, a colon, the same 41-byte listener maximum, a colon
// and the 6-byte `update` -- and each row is measured, so an implementation applying the 63-byte identifier bound
// to a comment body fails all three by length rather than passing on a substring search.
func TestMarkerHasNoIdentifierLengthBound(t *testing.T) {
	const specifiedMaximum = 101
	fixed := len(schema.MarkerPrefix) + 1 + 1 + len(theLongestOperationSpelling)
	if want := specifiedMaximum - fixed; want != 41+41 {
		t.Fatalf("the two name fields hold %d bytes at the ceiling, want 41+41", want)
	}

	for _, tc := range []struct {
		name     string
		instance int
	}{
		{"MarkerAt100 one byte below the specified ceiling", 40},
		{"MarkerAt101 the specified ceiling 11 + 41 + 1 + 41 + 1 + 6", 41},
		{"MarkerAt102 one byte above the specified ceiling", 42},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				instance, listener := strings.Repeat("i", tc.instance), strings.Repeat("l", 41)
				got := marker(instance, listener, theLongestOperationSpelling)
				if want := fixed + tc.instance + 41; len(got) != want {
					t.Fatalf("marker %q is %d bytes, want %d", got, len(got), want)
				}
				if !strings.HasSuffix(got, ":"+listener+":"+theLongestOperationSpelling) {
					t.Fatalf("marker %q lost an unbounded comment field", got)
				}
			},
		)
	}
}

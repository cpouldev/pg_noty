package config

import (
	"testing"

	"github.com/goccy/go-yaml"
)

// The fourth state, which is the library's decision rather than this package's: a key written with nothing
// after the colon.
//
// It sits apart from presence_test.go's three because it is not a state this package chose. YAML gives an
// author three spellings of "no value" and the decoder answers all three the same way, so a written-but-
// empty key is indistinguishable here from an absent one -- and the consequence belongs to the stage that
// decides what an omitted value means.

// TestAKeyWrittenWithNoValueDoesNotReachItsWrapper records the fourth state, which is the library's
// decision rather than this package's: measured on v1.19.2, the decoder does not call a
// NodeUnmarshaler at all for a key whose value is null, so `jitter:`, `jitter: null` and `jitter: ~`
// are indistinguishable from an absent key here.
//
// It is pinned rather than worked around because the consequence belongs to the stage that decides
// what an omitted value means, not to this one. **Step 8 must decide whether `version:` -- written,
// with nothing after the colon -- is R1's business**: today the key is present, so stage F reports
// no missing key, and the wrapper is unset, so stage H's Set gate skips it. An upgrade that starts
// calling the unmarshaler with a null node fails here first, which is the point.
func TestAKeyWrittenWithNoValueDoesNotReachItsWrapper(t *testing.T) {
	for _, written := range []string{"      jitter:\n", "      jitter: null\n", "      jitter: ~\n"} {
		t.Run(written, func(t *testing.T) {
			jitter := decodedListener(t, written).Retry.Value.Jitter

			if jitter.Set {
				t.Error("Set is true for a key written with no value; the library now calls the " +
					"unmarshaler with a null node, so re-derive what an omitted value means")
			}
		})
	}
}

// TestAnEmptyStringIsAValueRatherThanAnOmission is the other side of that guard: `""` is text the
// author wrote, and the decoder does call the wrapper for it. Without this row the pin above would
// read as "the decoder skips anything that looks empty", which is not what it measures.
func TestAnEmptyStringIsAValueRatherThanAnOmission(t *testing.T) {
	backoff := decodedListener(t, `      backoff: ""`+"\n").Retry.Value.Backoff

	if !backoff.Set {
		t.Error("Set is false for an explicitly empty string, which is a written value")
	}
	if !backoff.Valid() {
		t.Error("Valid() is false for an empty string, which is text the wrapper can read")
	}
	if backoff.value != "" {
		t.Errorf("read %q, want the empty string", backoff.value)
	}
}

// TestGoccySkipsANodeUnmarshalerForANullValue is the same measurement taken directly against the
// library, so a change in it is attributed to the library rather than to this package's raw
// tree.
func TestGoccySkipsANodeUnmarshalerForANullValue(t *testing.T) {
	tests := map[string]bool{
		"value: 1\n":    true,
		"value: \"\"\n": true,
		"value:\n":      false,
		"value: null\n": false,
		"value: ~\n":    false,
	}

	for document, wantCalled := range tests {
		t.Run(document, func(t *testing.T) {
			var into struct {
				Value Str `yaml:"value"`
			}

			if err := yaml.NodeToValue(parsedRoot(t, document), &into); err != nil {
				t.Fatalf("NodeToValue failed: %v", err)
			}
			if into.Value.Set != wantCalled {
				t.Errorf("the unmarshaler was called = %t, want %t", into.Value.Set, wantCalled)
			}
		})
	}
}

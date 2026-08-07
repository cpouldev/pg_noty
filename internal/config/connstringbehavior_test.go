package config

import "testing"

func TestOnlyTheConnectionStringsPasswordIsRedacted(t *testing.T) {
	for _, tc := range connectionRedactionCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactedConnectionString(tc.text); got != tc.want {
				t.Errorf("redactedConnectionString(%q) = %q, want %q",
					tc.text, got, tc.want)
			}
		})
	}
}

func TestPasswordSpansArriveRightToLeftAndNeverOverlap(t *testing.T) {
	carrying := []string{
		"postgres://u:pw@h/db?sslpassword=keypass",
		"host=db password=first password=second",
		"postgres://u:password=secret@h/db",
		"postgres://noty:s3cret@db.internal:5432/noty",
	}
	for _, text := range carrying {
		t.Run(text, func(t *testing.T) {
			spans := passwordSpans(text)
			if len(spans) == 0 {
				t.Fatalf("no password found in %q, so the ordering claim would hold vacuously", text)
			}
			for at, span := range spans {
				if span.from > span.to || int(span.to) > len(text) {
					t.Fatalf("span %d is %v, which is not a range of a %d-byte string",
						at, span, len(text))
				}
				if at > 0 && span.to > spans[at-1].from {
					t.Errorf("span %d ends at %d, past the start of the span before it (%d): applying them in order corrupts the text",
						at, span.to, spans[at-1].from)
				}
			}
		})
	}
}

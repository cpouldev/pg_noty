package schema

import (
	"strings"
	"testing"
)

// TestSanitizeStillStripsNULRatherThanRefusingIt pins the measured library behaviour WhyUnusable
// exists to keep unreachable.
//
// Measured on github.com/jackc/pgx/v5 v5.10.0, and byte-identical in v5.8.0 and v5.9.2: Sanitize
// deletes NUL rather than refusing it, so it is not injective -- "no\x00ty" and "noty" both render
// as one identifier naming `noty`. A configured name carrying a NUL would therefore name a
// different object than the operator wrote, silently. An upgrade that stops stripping fails here by
// name rather than downstream as a wrong identifier.
func TestSanitizeStillStripsNULRatherThanRefusingIt(t *testing.T) {
	for _, tc := range []struct{ name, written, want string }{
		{name: "a NUL between two runs", written: "no\x00ty", want: `"noty"`},
		{name: "the same name without the NUL", written: "noty", want: `"noty"`},
		{name: "a name that is only a NUL", written: "\x00", want: `""`},
		{name: "the empty name", written: "", want: `""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitized(tc.written); got != tc.want {
				t.Errorf("sanitized(%q) = %s, want %s", tc.written, got, tc.want)
			}
		})
	}

	if sanitized("no\x00ty") != sanitized("noty") {
		t.Error("sanitized no longer collapses a NUL-bearing name onto the name without it; " +
			"re-derive WhyUnusable's NUL refusal, which exists only because it does")
	}
	if want := `""`; sanitized("") != want {
		t.Errorf("sanitized(%q) = %s, want %s; PostgreSQL rejects a zero-length delimited "+
			"identifier, which is why WhyUnusable refuses an empty name", "", sanitized(""), want)
	}
}

// TestEachReasonToRefuseAnIdentifierHasItsOwnAnswer gives every refusal its own verdict rather than
// one shared negative. Each input satisfies every clause but the one it is named for, so a clause
// silently dropped fails its own row rather than passing on a neighbour's refusal.
func TestEachReasonToRefuseAnIdentifierHasItsOwnAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, written string
		want          IdentifierFault
	}{
		// 62, 63 and 64 bytes: the bound is 63, so only the last is over it. The equality row is the only
		// input separating `>` from `>=`.
		{name: "one byte under the limit", written: nameOfBytes(MaxIdentifierBytes - 1), want: IdentifierOK},
		{name: "exactly at the limit", written: nameOfBytes(MaxIdentifierBytes), want: IdentifierOK},
		{name: "one byte over the limit", written: nameOfBytes(MaxIdentifierBytes + 1), want: IdentifierTooLong},

		// Non-empty and well under the limit, so only the NUL clause can fire.
		{name: "a NUL between two runs", written: "no\x00ty", want: IdentifierHoldsNUL},
		{name: "a name that is only a NUL", written: "\x00", want: IdentifierHoldsNUL},

		// Neither over the limit nor NUL-bearing, so only the empty clause can fire.
		{name: "the empty name", written: "", want: IdentifierEmpty},

		// The precedence WhyUnusable documents, and the only input that can tell the two orders
		// apart: over the limit *and* NUL-bearing.
		{
			name:    "over the limit and holding a NUL",
			written: nameOfBytes(MaxIdentifierBytes+7) + "\x00",
			want:    IdentifierHoldsNUL,
		},

		{name: "an ordinary name", written: "event_queue", want: IdentifierOK},
		{name: "a name a metacharacter makes hostile but not unusable", written: `no"ty`, want: IdentifierOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WhyUnusable(tc.written); got != tc.want {
				t.Errorf("WhyUnusable(%q) = %q, want %q", tc.written, got, tc.want)
			}
		})
	}
}

// nameOfBytes writes a name of exactly the given byte length, opening with a letter so it is a name
// a configuration could plausibly carry.
func nameOfBytes(length int) string {
	return "s" + strings.Repeat("x", length-1)
}

// TestAnIdentifierAtTheByteLimitRoundTripsAndOneOverIsRefused is the bound's own row, asserted
// through the quoting authority rather than through the classifier alone: 63 bytes in, the same 63
// bytes plus the two quotes out.
//
// Over the limit is a refusal rather than a truncation because PostgreSQL truncates silently with
// only a NOTICE, so two configured names sharing a 63-byte prefix would name one object -- the same
// non-injectivity the NUL strip has, arrived at by a different route.
func TestAnIdentifierAtTheByteLimitRoundTripsAndOneOverIsRefused(t *testing.T) {
	atLimit := nameOfBytes(MaxIdentifierBytes)

	rendered, fault := Quoted(atLimit)
	if fault != IdentifierOK {
		t.Fatalf("Quoted refused a %d-byte name as %q, and the limit is %d bytes",
			len(atLimit), fault, MaxIdentifierBytes)
	}
	if want := len(atLimit) + 2; len(rendered) != want {
		t.Errorf("a %d-byte name rendered to %d bytes, want %d: the name unchanged inside two quotes",
			len(atLimit), len(rendered), want)
	}
	assertReadsBackAs(t, rendered, atLimit)

	if _, fault := Quoted(nameOfBytes(MaxIdentifierBytes + 1)); fault != IdentifierTooLong {
		t.Errorf("Quoted answered %q for a %d-byte name, want %q",
			fault, MaxIdentifierBytes+1, IdentifierTooLong)
	}
}

// TestQualifiedAnswersTheSchemaPartBeforeTheNamePart pins the precedence qualified documents, over
// the only input that can tell the two orders apart: both parts unusable, and for different
// reasons.
func TestQualifiedAnswersTheSchemaPartBeforeTheNamePart(t *testing.T) {
	if _, fault := Qualified("", "no\x00ty"); fault != IdentifierEmpty {
		t.Errorf("Qualified answered %q, want the schema part's own %q", fault, IdentifierEmpty)
	}
	if _, fault := Qualified("noty", "no\x00ty"); fault != IdentifierHoldsNUL {
		t.Errorf("Qualified answered %q for a usable schema and an unusable name, want %q",
			fault, IdentifierHoldsNUL)
	}
}

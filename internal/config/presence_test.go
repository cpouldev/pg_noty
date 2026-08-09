package config

import (
	"testing"
)

// The four states a decoded value can be in, and why telling them apart is the whole reason the
// wrappers exist: absent, written and read, written and unreadable, and -- the one YAML gives an
// author two spellings of -- written with nothing after the colon.
//
// AC #22 is what rests on the first two being distinct. `jitter: false`, `max_attempts: 0` and
// `initial_interval: 0s` are values an author wrote on purpose, and Go's zero value cannot say so: a
// merge reading the value alone inherits the default over all three. Step 10's merge therefore reads
// Set, and this file is where Set means what that merge needs it to mean.
//
// One table per falsy *kind* rather than one shared table, because the three wrappers reach their zero
// differently -- a flag is read from two literals, a count from strconv, a duration from
// time.ParseDuration -- so a conversion that mistook its own zero for a failure to read would be a
// different defect in each. Every kind the contract declares whose zero is indistinguishable from
// absence has a table here, which is what stops a fourth one being added without one.

// presenceCase is one value written into a complete document, with the state every wrapper beneath
// the raw tree should be left in.
//
// The document is decoded through stage G rather than by handing a node to a wrapper, because what
// AC #22 needs is what a *file* leaves behind -- and the difference between "the key is absent" and
// "the key is written" is the decoder's to make, not the wrapper's (a wrapper is never called for an
// absent key at all).
type presenceCase struct {
	name string
	// written is the retry block the case writes, or the empty string for a listener that writes no
	// retry block at all.
	written string
	wantSet bool
	// wantValue is what the field holds afterwards, so a case cannot pass by leaving a value the
	// merge would then have to guess about.
	wantValue string
}

// TestAbsenceIsDistinguishableFromAnExplicitlyFalsyValue is AC #22's precondition as one table over
// the boundary the merge is decided on. Every row is a value whose Go zero value is
// indistinguishable from absence, which is exactly the set of rows that can falsify Set.
func TestAbsenceIsDistinguishableFromAnExplicitlyFalsyValue(t *testing.T) {
	tests := []presenceCase{
		{name: "an absent boolean", written: "", wantSet: false, wantValue: "false"},
		{name: "an explicit false", written: "      jitter: false\n", wantSet: true, wantValue: "false"},
		{name: "an explicit true", written: "      jitter: true\n", wantSet: true, wantValue: "true"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := decodedListener(t, tc.written)

			if got := raw.Retry.Value.Jitter; got.Set != tc.wantSet {
				t.Errorf("Set = %t, want %t: a merge cannot preserve an explicit false without it", got.Set, tc.wantSet)
			}
			if got := boolText(raw.Retry.Value.Jitter.value); got != tc.wantValue {
				t.Errorf("read %s, want %s", got, tc.wantValue)
			}
		})
	}
}

// TestAnExplicitZeroIsDistinguishableFromAbsence is the same boundary for an integer, which AC #22
// names alongside `jitter: false` because zero is the value R14 refuses -- so a merge that read it
// as absence would silently substitute the default and the rule would never fire.
func TestAnExplicitZeroIsDistinguishableFromAbsence(t *testing.T) {
	tests := []presenceCase{
		{name: "an absent integer", written: "", wantSet: false, wantValue: "0"},
		{name: "an explicit zero", written: "      max_attempts: 0\n", wantSet: true, wantValue: "0"},
		{name: "a written integer", written: "      max_attempts: 10\n", wantSet: true, wantValue: "10"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := decodedListener(t, tc.written)

			if got := raw.Retry.Value.MaxAttempts; got.Set != tc.wantSet {
				t.Errorf("Set = %t, want %t", got.Set, tc.wantSet)
			}
			if got := intText(raw.Retry.Value.MaxAttempts.value); got != tc.wantValue {
				t.Errorf("read %s, want %s", got, tc.wantValue)
			}
		})
	}
}

// TestAnExplicitZeroDurationIsDistinguishableFromAbsence is the same boundary for a duration, which is
// the third of the three falsy kinds AC #22 spans and the one the earlier round left out: `0s` renders as
// Go's zero exactly as `0` and `false` do, so a merge reading the value alone substitutes the built-in
// default over an author who wrote "do not wait".
//
// It is a separate case from the integer's rather than a row in it because the wrappers differ: a
// duration is text the wrapper parses, so `0s` also proves the conversion accepts a zero rather than
// treating it as a failure to read -- which would make the value Set and *not* Valid, a fourth state the
// merge would then skip.
func TestAnExplicitZeroDurationIsDistinguishableFromAbsence(t *testing.T) {
	tests := []presenceCase{
		{name: "an absent duration", written: "", wantSet: false, wantValue: "0s"},
		{name: "an explicit zero duration", written: "      initial_interval: 0s\n", wantSet: true, wantValue: "0s"},
		{name: "a written duration", written: "      initial_interval: 250ms\n", wantSet: true, wantValue: "250ms"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			interval := decodedListener(t, tc.written).Retry.Value.InitialInterval

			if interval.Set != tc.wantSet {
				t.Errorf("Set = %t, want %t: a merge cannot preserve an explicit zero duration without it",
					interval.Set, tc.wantSet)
			}
			if got := durText(interval.value); got != tc.wantValue {
				t.Errorf("read %s, want %s", got, tc.wantValue)
			}
			// Absence must not look like a refusal, or stage H would skip a key that is merely omitted;
			// and an explicit zero must be Valid, or the merge would skip a value the author wrote.
			if interval.Valid() != tc.wantSet {
				t.Errorf("Valid() = %t, want %t", interval.Valid(), tc.wantSet)
			}
		})
	}
}

// TestAValueTheWrapperCouldNotReadIsSetButNotValid is the third state, and the reason it exists is
// the no-double-report rule: stage H evaluates a field only when its wrapper read a value, so a
// `7d` timeout must be Set (the author wrote it) and not Valid (there is nothing to judge). Without
// the second flag stage H would report a range violation about a zero it invented.
func TestAValueTheWrapperCouldNotReadIsSetButNotValid(t *testing.T) {
	listener, diags := listenerReporting(t, "      initial_interval: 7d\n")
	if len(diags) != 1 {
		t.Fatalf("decode reported %q, want the one duration it could not read", messagesOf(diags))
	}

	interval := listener.Retry.Value.InitialInterval
	if !interval.Set {
		t.Error("Set is false for a key the author wrote")
	}
	if interval.Valid() {
		t.Error("Valid() is true for a value the wrapper could not read")
	}
}

// TestAnAbsentValueIsNeitherSetNorValid keeps the two flags from being read as one. Absence is not a
// refusal: nothing is wrong with a file that omits an optional key, and a stage that treated
// "not valid" as "report something" would report every default.
func TestAnAbsentValueIsNeitherSetNorValid(t *testing.T) {
	raw := decodedListener(t, "")

	jitter := raw.Retry.Value.Jitter
	if jitter.Set || jitter.Valid() {
		t.Errorf("Set = %t and Valid() = %t for an absent key, want both false", jitter.Set, jitter.Valid())
	}
}

// decodedListener runs the pipeline's decode over a listener writing retryLines inside its retry
// block, and answers the one listener it decoded.
//
// It goes through stage G rather than calling the wrapper directly because that is the only way to
// exercise absence: an absent key is a call the decoder never makes.
func decodedListener(t *testing.T, retryLines string) rawListener {
	t.Helper()

	listener, diags := listenerReporting(t, retryLines)
	if len(diags) != 0 {
		t.Fatalf("decoding reported %q for a value every wrapper can read", messagesOf(diags))
	}
	return listener
}

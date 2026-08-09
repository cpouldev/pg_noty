package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestFingerprintCombinesBothDefinitionsWithTheSpecifiedComposition(t *testing.T) {
	const triggerDefinition = "CREATE TRIGGER orders_insert"
	const functionDefinition = "CREATE FUNCTION emit_order()"
	sum := sha256.Sum256([]byte(triggerDefinition + "\x00" + functionDefinition))
	want := "1:" + hex.EncodeToString(sum[:])
	if got := fingerprint(triggerDefinition, functionDefinition); got != want {
		t.Errorf("fingerprint = %q, want %q", got, want)
	}
}

func TestFingerprintSeparatesPairsAPlainConcatenationWouldCollide(t *testing.T) {
	// PostgreSQL text cannot carry a NUL, so the separator cannot occur in either reading.
	if left, right := fingerprint("ab", "c"), fingerprint("a", "bc"); left == right {
		t.Fatalf("NUL-separated fingerprints collide: %q", left)
	}
}

// rebaselinedFingerprint is a genuinely different composition over the same two readings -- the
// definitions folded in the other order -- carried under its own tag. It exists so the test below
// can change *what is folded* rather than only the tag: `"2:" + digest` differs from `"1:" + digest`
// for any digest at all, so an assertion built that way passes without the composition ever moving
// and the clause reads as covered. Nothing outside this file calls it, and the single-declaration
// scan reads production sources only, so this is a counterexample rather than a second authority.
const rebaselinedFingerprintVersion = "2:"

func rebaselinedFingerprint(triggerDefinition, functionDefinition string) string {
	sum := sha256.Sum256([]byte(functionDefinition + "\x00" + triggerDefinition))
	return rebaselinedFingerprintVersion + hex.EncodeToString(sum[:])
}

// fingerprintReading is the conclusion an operator draws from a stored fingerprint and a fresh one,
// and it is the whole of what the version tag buys. Production compares fingerprints for equality
// and nothing more (classify.go:122), so this reading exists at the operator's level: same tag with
// a different digest is the catalog having changed under us, while a different tag says this
// package rebaselined its own composition and every mismatch in the run has that one cause.
type fingerprintReading string

const (
	fingerprintsAgree      fingerprintReading = "agree"
	readingsDrifted        fingerprintReading = "the readings drifted"
	compositionRebaselined fingerprintReading = "the composition was rebaselined"
)

func readFingerprints(stored, fresh string) fingerprintReading {
	switch {
	case stored == fresh:
		return fingerprintsAgree
	case fingerprintTag(stored) != fingerprintTag(fresh):
		return compositionRebaselined
	default:
		return readingsDrifted
	}
}

func fingerprintTag(value string) string {
	tag, _, tagged := strings.Cut(value, ":")
	if !tagged {
		return ""
	}
	return tag + ":"
}

func fingerprintDigest(value string) string {
	return strings.TrimPrefix(value, fingerprintTag(value))
}

func TestFingerprintVersionTagMarksACompositionRebaseline(t *testing.T) {
	const wantVersion = "1:"
	if fingerprintVersion != wantVersion {
		t.Fatalf("fingerprint version = %q, want specification literal %q", fingerprintVersion, wantVersion)
	}
	const trigger, function = "CREATE TRIGGER orders_insert", "CREATE FUNCTION emit_order()"
	stored := fingerprint(trigger, function)
	if !strings.HasPrefix(stored, wantVersion) {
		t.Fatalf("stored fingerprint = %q, want recognisable version prefix %q", stored, wantVersion)
	}

	rebaselined := rebaselinedFingerprint(trigger, function)
	if fingerprintDigest(rebaselined) == fingerprintDigest(stored) {
		t.Fatalf("the rebaselined composition folds %q and %q to the shipped digest, so every row "+
			"below changes the tag without changing what is fingerprinted", trigger, function)
	}
	if fingerprintTag(rebaselined) == fingerprintTag(stored) {
		t.Fatalf("rebaselined fingerprint %q carries the shipped tag %q, so the tag did not move "+
			"with the composition", rebaselined, fingerprintTag(stored))
	}

	for _, tc := range []struct {
		name, fresh string
		want        fingerprintReading
	}{
		{name: "the same readings under the same composition", fresh: stored, want: fingerprintsAgree},
		// One definition edited on the server. The tag is unchanged, so this is the catalog moving.
		{name: "an edited definition under the same composition",
			fresh: fingerprint(trigger, function+" -- edited in psql"), want: readingsDrifted},
		// The rebaseline done right: recognisable at a glance, and on every object at once.
		{name: "a rebaselined composition carrying its own tag",
			fresh: rebaselined, want: compositionRebaselined},
		// The rebaseline the tag exists to prevent. The composition moved and the tag did not, so a
		// whole registry of unchanged objects reports drift with no cause an operator can name.
		{name: "a rebaselined composition left under the shipped tag",
			fresh: fingerprintVersion + fingerprintDigest(rebaselined), want: readingsDrifted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := readFingerprints(stored, tc.fresh); got != tc.want {
				t.Fatalf("readFingerprints(%q, %q) = %q, want %q", stored, tc.fresh, got, tc.want)
			}
		})
	}
}

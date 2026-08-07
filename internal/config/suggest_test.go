package config

import "testing"

// The did-you-mean rule, on its own: closest declared name within distance 2, ties broken
// lexicographically, no suggestion beyond the threshold (AC #9). What a diagnostic does with the
// answer is shapecheck_test.go's subject.
//
// Every distance below is counted from the two names rather than recorded from a run, and the
// candidate lists are the contract's own key lists wherever a real one reaches the case.

// listenerNames is the contract's own vocabulary for a listener, read from the table through
// schema_test.go's one reader of it rather than written out again, so a key added to the contract
// widens these cases with it.
func listenerNames() []string { return keyNames(schemaLevels[levelListener]) }

// payloadNames is the same for a payload, which is the level AC #9's two examples are written at.
func payloadNames() []string { return keyNames(schemaLevels[levelPayload]) }

func TestTheClosestNameWithinTheThresholdIsSuggested(t *testing.T) {
	for _, tc := range suggestionCases() {
		t.Run(tc.name, func(t *testing.T) {
			got, found := closestName(tc.written, tc.candidates)

			if found != tc.wantFound {
				t.Fatalf("closestName(%q) found = %t, want %t (suggested %q)", tc.written, found, tc.wantFound, got)
			}
			if found && got != tc.want {
				t.Errorf("closestName(%q) = %q, want %q", tc.written, got, tc.want)
			}
		})
	}
}

// TestTheSuggestionThresholdIsTheOnePrecedentSets pins the boundary as a number as well as
// through the cases above, because the threshold is a documented contract term: the CLI framework
// already approved for this project uses 2, and the Description states it.
func TestTheSuggestionThresholdIsTheOnePrecedentSets(t *testing.T) {
	if suggestionThreshold != 2 {
		t.Errorf("suggestionThreshold = %d, want 2, the documented rule", suggestionThreshold)
	}
}

// TestTheEditDistanceIsSymmetricAndZeroOnlyForOneName covers the two properties the tie-break
// rests on. Without symmetry the answer would depend on which name the caller passed first, and
// without the zero case a name equal to a candidate would be reported as an edit away from it.
func TestTheEditDistanceIsSymmetricAndZeroOnlyForOneName(t *testing.T) {
	names := append(listenerNames(), "colums", "", "módé")

	for _, a := range names {
		for _, b := range names {
			forwards, backwards := editDistance(a, b), editDistance(b, a)

			if forwards != backwards {
				t.Errorf("editDistance(%q,%q) = %d but the reverse is %d", a, b, forwards, backwards)
			}
			if sameName := a == b; sameName != (forwards == 0) {
				t.Errorf("editDistance(%q,%q) = %d, which does not agree with whether they are one name",
					a, b, forwards)
			}
		}
	}
}

// TestTheDistanceToAnEmptyNameIsItsLength pins the base row of the table, which is the arm no
// pair of declared names reaches: every candidate is non-empty, so a written name of no length
// is the only input that exercises it.
func TestTheDistanceToAnEmptyNameIsItsLength(t *testing.T) {
	// Four runes written in six bytes, so a byte-counted table would answer six.
	const written = "módé"

	if got := editDistance(written, ""); got != 4 {
		t.Errorf("editDistance(%q,\"\") = %d, want 4, its length in runes", written, got)
	}
}

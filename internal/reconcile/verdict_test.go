package reconcile

import "testing"

const verdictCount = 3

func TestVerdictsAreClosedAndMapToSpecificationPinnedExitCodes(t *testing.T) {
	// Range the declared vocabulary and pin its contract size instead of asserting a copied
	// list.
	if got := len(verdicts); got != verdictCount {
		t.Fatalf("verdicts declares %d values, want %d", got, verdictCount)
	}

	// The exit codes are specification-mandated literals, not expectations derived from
	// ExitCode.
	wantCodes := map[Verdict]int{
		"clean":           0,
		"changes_pending": 2,
		"error":           1,
	}
	seen := map[Verdict]bool{}
	for _, verdict := range verdicts {
		if verdict == "" {
			t.Fatal("the zero Verdict must not be a declared verdict")
		}
		if seen[verdict] {
			t.Fatalf("verdict %q appears twice", verdict)
		}
		seen[verdict] = true
		want, known := wantCodes[verdict]
		if !known {
			t.Fatalf("undeclared verdict %q appeared in the closed set", verdict)
		}
		if got := verdict.ExitCode(); got != want {
			t.Errorf("%q.ExitCode() = %d, want specification literal %d", verdict, got, want)
		}
	}
	if got := len(seen); got != len(wantCodes) {
		t.Fatalf("declared verdict set has %d members, want %d", got, len(wantCodes))
	}
}

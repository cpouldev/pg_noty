package config

import (
	"strings"
	"testing"
)

// The containment invariant is quantified over value layouts, YAML key
// spellings, generated secret bytes, and every applicable redaction branch.
func TestD3ForbidsUnderRedactionInEveryShapeAndSpellingOnEveryApplicableBranch(t *testing.T) {
	secret := secretMarkedOnEveryLine("plain")

	for _, layout := range secretLayouts {
		for _, spelling := range keySpellings {
			for _, planted := range documentsHiding(secret, layout, spelling) {
				t.Run(planted.where+", "+planted.branch, func(t *testing.T) {
					assertNothingLeaks(t, planted)
				})
			}
		}
	}
}

func assertNothingLeaks(t *testing.T, planted plantedSecret) {
	t.Helper()

	ran, pathAware := branchTaken(planted)
	if pathAware != planted.pathAware {
		t.Fatalf("the document takes the %s branch instead of the %s one it is written for:\n%s",
			ran, planted.branch, planted.text)
	}
	assertNoMarkerSurvives(t, planted, ran)
}

// branchTaken is the redaction branch a planted document actually reaches. It is a property of the
// document's bytes rather than of the layout that wrote them, and the two can disagree: one closing
// bracket in a generated tail closes the container a fallback-only layout deliberately left open,
// the document parses after all, and the plant runs down the branch it declared it would not reach.
// Both subjects measure it here, so neither can name a branch that did not run.
func branchTaken(planted plantedSecret) (name string, pathAware bool) {
	root, diags := parseDocument(newSource("", []byte(planted.text)))
	if pathsAreResolvable(root, diags) {
		return "path-aware", true
	}
	return "key-scoped fallback", false
}

// assertNoMarkerSurvives is the containment claim itself, reported against the branch that ran.
func assertNoMarkerSurvives(t *testing.T, planted plantedSecret, ran string) {
	t.Helper()

	rendered := renderEveryLineOf(planted.text)
	if !strings.Contains(rendered, "listeners.yaml:") {
		t.Fatalf("%s: nothing was rendered, so the search would pass vacuously:\n%s",
			planted.where, planted.text)
	}
	for _, marker := range planted.markers {
		if strings.Contains(rendered, marker) {
			t.Errorf("%s: the %s branch quoted %s\nsource:\n%s\nrendered:\n%s",
				planted.where, ran, marker, planted.text, rendered)
		}
	}
}

func FuzzRenderedTextNeverQuotesASecret(f *testing.F) {
	forEachContainmentSeed(func(tail string, shape, spelling uint8) {
		f.Add(tail, shape, spelling)
	})

	f.Fuzz(func(t *testing.T, tail string, shape, spelling uint8) {
		assertTheGridContains(t, tail, shape, spelling)
	})
}

// forEachContainmentSeed enumerates the seed grid once, for the target that adds the seeds and for
// the reconciliation that runs them as this requirement's subject. Neither may enumerate it
// separately, or the run reconciled against would not be the run the target performs.
func forEachContainmentSeed(visit func(tail string, shape, spelling uint8)) {
	for _, tail := range seedTails() {
		for shape := range secretLayouts {
			for spelling := range keySpellings {
				visit(tail, uint8(shape), uint8(spelling))
			}
		}
	}
}

// assertTheGridContains is the fuzz body: the ordinals the target is handed, resolved against the
// grid they index.
func assertTheGridContains(t *testing.T, tail string, shape, spelling uint8) {
	t.Helper()

	assertPlantedSecretsAreContained(t, tail,
		secretLayouts[int(shape)%len(secretLayouts)],
		keySpellings[int(spelling)%len(keySpellings)])
}

// runTheContainmentSeedCorpus is what running the security requirement's named assertion means for
// the reconciliation, which cannot call a Fuzz target with a *testing.T.
func runTheContainmentSeedCorpus(t *testing.T) {
	forEachContainmentSeed(func(tail string, shape, spelling uint8) {
		assertTheGridContains(t, tail, shape, spelling)
	})
}

// assertPlantedSecretsAreContained is the body the fuzz target and the committed-corpus test both
// run, so neither subject can assert something the other does not.
func assertPlantedSecretsAreContained(t *testing.T, tail string, layout secretLayout, key keySpelling) {
	t.Helper()

	for _, planted := range documentsHiding(secretMarkedOnEveryLine(tail), layout, key) {
		ran, pathAware := branchTaken(planted)
		if pathAware {
			// The document parses, so which bytes are sensitive can be read from it -- and are, in
			// place of the layout's belief about where it hid one. A generated tail restructures the
			// document freely: a `#` at the head of a value turns its line into a comment, a closer
			// inside a flow container ends it early, and the markers then sit outside every value the
			// schema declares sensitive. Requiring their absence would report a leak the redactor did
			// not commit.
			//
			// The trigger is the branch that ran rather than the branch the layout expected, which is
			// what lets the generator write every byte class instead of excluding the ones that can
			// move a marker: the oracle is strengthened where the payload used to be weakened. Both
			// sides of it are pinned by TestAGeneratedTailThatClosesItsContainerLeavesItsContract.
			assertNoMarkerInsideASensitiveValueSurvives(t, planted)
			continue
		}
		// The document does not parse, so nothing can be read from it -- which is the state the
		// key-scoped fallback exists for, and its contract is the layout's: every line a sensitive
		// key name reaches goes, whatever it turned out to hold. So every marker is asserted, on
		// every plant that reached this branch including the ones written for the other.
		assertNoMarkerSurvives(t, planted, ran)
	}
}

// These named guards keep the two former skipped leak classes independently
// reproducible in addition to their rows in the deterministic grid.
func TestFallbackRedactsAKeyShapedContinuationOfAnUnclosedSensitiveScalar(t *testing.T) {
	source := "secrets: \"\nother: " + leakSentinel + "CONTINUATION\nlisteners: [one, two\n"

	if rendered := renderEveryLineOf(source); strings.Contains(rendered, leakSentinel) {
		t.Errorf("the fallback kept an unclosed quoted scalar's continuation:\n%s", rendered)
	}
}

func TestPathAwareRedactionCoversASensitiveLeafUnderAnUndeclaredParent(t *testing.T) {
	source := "databse:\n  url: postgres://noty:" + leakSentinel + "MISSPELLED@db.internal/noty\n"

	if rendered := renderEveryLineOf(source); strings.Contains(rendered, leakSentinel) {
		t.Errorf("a sensitive key under an undeclared parent was not redacted:\n%s", rendered)
	}
}

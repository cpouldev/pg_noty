package reconcile

import (
	"strings"
	"testing"
)

func TestClassificationRefusesEveryOwnershipDisagreementForBothObjectKinds(t *testing.T) {
	// Each source satisfies every ownership clause except its named disagreement (isolate-each-clause-of-a-multi-clause-guard.md).
	for _, kind := range []string{"trigger", "function"} {
		for _, disagreement := range disagreements {
			t.Run(kind+"/"+string(disagreement), func(t *testing.T) {
				source := ownershipDisagreementSource(t, kind, disagreement)
				got := classifyPair(source)
				if got.Refusal == nil || got.Action != nil || !strings.Contains(got.Refusal.Message(), kind) {
					t.Fatalf("%s %q classification = %+v, want named refusal and no action", kind, disagreement, got)
				}
			})
		}
	}
}

func ownershipDisagreementSource(t *testing.T, kind string, disagreement Disagreement) pairSources {
	source := ownedPairSource(t, Pair{Listener: "orders", Operation: "insert"})
	marker := generatedMarker(t, "alpha", "payments", "insert")
	foreign := generatedMarker(t, "bravo", "orders", "insert")
	if kind == "function" {
		source.Observed.TriggerPresent = false
	}
	switch disagreement {
	case DisagreementMarkerAbsent:
		if kind == "trigger" {
			source.Observed.Trigger.Marker = nil
		} else {
			source.Observed.Function.Marker = nil
		}
	case DisagreementForeignInstance:
		if kind == "trigger" {
			source.Observed.Trigger.Marker = &foreign
		} else {
			source.Observed.Function.Marker = &foreign
		}
	case DisagreementAnotherPair:
		if kind == "trigger" {
			source.Observed.Trigger.Marker = &marker
		} else {
			source.Observed.Function.Marker = &marker
		}
	case DisagreementNoRegistryRow:
		source.Recorded.TriggerPresent = false
	}
	return source
}

package reconcile

import "testing"

// TestARefusalOnOneListenerSuppressesASiblingsDestructiveAction is the second clause of the
// refusal rule at the plan layer: a refusal anywhere in the run stops the whole run, so the genuinely
// destructive action already planned for a *different* listener is not carried in the plan either.
// diff.go's `result.Actions = nil` in finaliseDiff is the one line that makes it true, and no case
// before this one reached it -- the refusing pair produces no action of its own, so a plan holding
// only that pair is empty whether the line is there or not.
//
// The behavioural half of that rule -- that Apply performs neither that destruction nor any other
// database change, with every flag that would otherwise permit it supplied -- needs Apply and is
// TestAnUnmarkedCollisionLeavesBothListenersUnchanged in collisionapply_integration_test.go,
// where the deferral in ownershipsurvival_integration_test.go is also discharged. What is
// asserted here is the plan; what is deferred is its execution.
func TestARefusalOnOneListenerSuppressesASiblingsDestructiveAction(t *testing.T) {
	doomed := ownedPairSource(t, Pair{Listener: "doomed", Operation: "delete"})
	// Absent from the configuration, so its own classification is a drop: destruction the run would
	// perform, and the nearest input that must survive the refusal guard is this run without one.
	doomed.Desired.ListenerPresent = false

	permitted := diff(diffInput{Recorded: []recordedPair{doomed.Recorded},
		Observed: []observedPair{doomed.Observed}, Instance: doomed.Instance})
	if len(permitted.Actions) != 1 || !permitted.Destructive() || permitted.Verdict != VerdictChangesPending {
		t.Fatalf("sibling-only plan = %+v, want one destructive drop to be suppressed below", permitted)
	}
	assertActionKind(t, permitted.Actions[0], doomed.pair(), ActionDrop)

	assertRefusedPlanCarriesNoAction(t, diff(refusedRunWithSibling(t, doomed)))
}

// refusedRunWithSibling adds the collided listener: this instance's exact derived object carrying no
// ownership marker, on a listener of its own, so the refusal and the destruction belong to different
// listeners and neither can be mistaken for the other's pair.
func refusedRunWithSibling(t *testing.T, doomed pairSources) diffInput {
	t.Helper()
	collided := ownedPairSource(t, Pair{Listener: "collided", Operation: "insert"})
	collided.Observed.Trigger.Marker = nil
	return diffInput{
		Desired:   []desiredPair{collided.Desired},
		Recorded:  []recordedPair{collided.Recorded, doomed.Recorded},
		Observed:  []observedPair{collided.Observed, doomed.Observed},
		Listeners: []desiredListener{{Name: collided.Desired.Pair.Listener, SpecHash: collided.Desired.SpecHash, Enabled: true}},
		Instance:  collided.Instance,
	}
}

func assertRefusedPlanCarriesNoAction(t *testing.T, refused PlanResult) {
	t.Helper()
	if len(refused.Refusals) != 1 || refused.Verdict != VerdictError {
		t.Fatalf("refused plan = %+v, want one ownership refusal and the error verdict", refused)
	}
	if len(refused.Actions) != 0 || refused.Destructive() {
		t.Fatalf("refused plan still carries %#v; a refusal stops the whole run, so the other "+
			"listener's drop must not survive it", refused.Actions)
	}
}

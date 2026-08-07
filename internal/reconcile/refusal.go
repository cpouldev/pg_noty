package reconcile

import "fmt"

// Refusal is a complete operator-facing stop reason. Its fields stay private so callers use this
// file's seven constructors instead of assembling a competing refusal spelling.
type Refusal struct {
	message     string
	remediation string
}

func (r Refusal) Message() string { return r.message }

func (r Refusal) Remediation() string { return r.remediation }

// ownershipRefusal names the object, the marker evidence observed for it, and the remediation.
func ownershipRefusal(identity string, marker *string) Refusal {
	evidence := "no ownership marker was found"
	if marker != nil {
		evidence = fmt.Sprintf("found ownership marker %q", *marker)
	}
	return Refusal{message: fmt.Sprintf("refusing %s: %s", identity, evidence),
		remediation: "leave the object untouched or remove the conflicting marker only after confirming its owner"}
}

// absentBootstrapRefusal answers a service schema that has not been bootstrapped.
func absentBootstrapRefusal(subject string) Refusal {
	return Refusal{message: fmt.Sprintf("cannot reconcile: required bootstrap object %s is absent", subject),
		remediation: "run bootstrap for this service schema before reconciling"}
}

// droppedTargetRefusal answers a listener whose target table no longer exists.
func droppedTargetRefusal(listener, target string) Refusal {
	return Refusal{message: fmt.Sprintf("listener %q targets missing table %s", listener, target),
		remediation: "restore the target table or remove the listener from configuration"}
}

// retargetedListenerRefusal answers a listener whose configured table is not the one its registry
// row records. Without it the replaces are generated and applied against the *old* table, the spec
// hash is rewritten while the target is not, and every later plan reports clean.
//
// It refuses rather than migrating: moving a listener is two destructive halves, and the operator
// owns that decision.
func retargetedListenerRefusal(listener, recorded, configured string) Refusal {
	return Refusal{
		message: fmt.Sprintf("listener %q is recorded against %s but configured against %s",
			listener, recorded, configured),
		remediation: fmt.Sprintf("remove listener %q, apply so its triggers on %s are dropped, then "+
			"add it back against %s; or restore the configured table to %s",
			listener, recorded, configured, recorded),
	}
}

// tableLockTimeoutRefusal answers a table lock the bounded wait could not take.
func tableLockTimeoutRefusal(table, blocker string) Refusal {
	return Refusal{message: fmt.Sprintf("timed out waiting for table lock on %s; blocker backend is %s", table, blocker),
		remediation: fmt.Sprintf("wait for backend %s or end its transaction, then retry reconciliation", blocker)}
}

// runLockWaitExpiredRefusal answers a run lock still held when the bounded wait expired.
func runLockWaitExpiredRefusal(instance, holder string) Refusal {
	return Refusal{message: fmt.Sprintf("timed out waiting for reconcile run lock for instance %q; holder backend is %s", instance, holder),
		remediation: fmt.Sprintf("wait for backend %s to finish or investigate that instance before retrying", holder)}
}

// unknownDeferredKindRefusal answers a deferred check kind this version does not understand.
func unknownDeferredKindRefusal(kind string) Refusal {
	return Refusal{message: fmt.Sprintf("cannot validate unknown deferred check kind %q", kind),
		remediation: fmt.Sprintf("upgrade the reconciler to a version that understands %q or remove that check", kind)}
}

package reconcile

// StopReason identifies why a reconciliation run did not proceed.
type StopReason string

const (
	StopOwnership          StopReason = "ownership"
	StopPermission         StopReason = "permission"
	StopApproval           StopReason = "approval"
	StopValidation         StopReason = "validation"
	StopRunLockWaitExpired StopReason = "run_lock_wait_expired"
	StopTargetDropped      StopReason = "target_dropped"
)

// stopReasons is the closed vocabulary pinned at six members by decision_test.go.
var stopReasons = []StopReason{
	StopOwnership,
	StopPermission,
	StopApproval,
	StopValidation,
	StopRunLockWaitExpired,
	StopTargetDropped,
}

// Decision is the approval result for a plan that has at least one action.
type Decision struct {
	Proceed bool
	Stopped StopReason
}

// Decide evaluates one changed plan. It is called only for a plan carrying at least one action: a
// clean plan proceeds before consulting it, which is why this grid has sixteen cells, not
// twenty-four. Interactive is internal/cli's supplied capability to obtain confirmation. Without
// prior approval it lets the decision continue to that confirmation boundary; internal/cli must
// obtain it before executing the apply. This package never detects a terminal or prompts.
func Decide(destructive, permitted, approved, interactive bool) Decision {
	if destructive && !permitted {
		return Decision{Stopped: StopPermission}
	}
	if !approved && !interactive {
		return Decision{Stopped: StopApproval}
	}
	return Decision{Proceed: true}
}

// noActionDecision is the logical no-action result, outside Decide's changed-plan precondition.
func noActionDecision() Decision {
	return Decision{Proceed: true}
}

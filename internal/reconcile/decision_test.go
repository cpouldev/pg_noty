package reconcile

import "testing"

const stopReasonCount, approvalGridSize = 6, 2 * 2 * 2 * 2

func TestStopReasonsAreClosed(t *testing.T) {
	// Range the declared vocabulary and pin its contract size instead of asserting a copied
	// list.
	if got := len(stopReasons); got != stopReasonCount {
		t.Fatalf("stopReasons declares %d values, want %d", got, stopReasonCount)
	}
	wantReasons := map[StopReason]bool{"ownership": true, "permission": true, "approval": true, "validation": true, "run_lock_wait_expired": true, "target_dropped": true}
	seen := map[StopReason]bool{}
	for _, reason := range stopReasons {
		if reason == "" {
			t.Fatal("the zero StopReason must mean proceed, not a stopped run")
		}
		if seen[reason] {
			t.Fatalf("stop reason %q appears twice", reason)
		}
		if !wantReasons[reason] {
			t.Fatalf("undeclared stop reason %q appeared in the closed set", reason)
		}
		seen[reason] = true
	}
	if got, want := len(seen), len(wantReasons); got != want {
		t.Fatalf("declared stop-reason set has %d members, want %d", got, want)
	}
}

func TestTheApprovalGridIsCrossedOnAllFourAxes(t *testing.T) {
	// crossedApprovalCases constructs the product rather than transcribing rows.
	cases := crossedApprovalCases()
	if got, want := len(cases), approvalGridSize; got != want {
		t.Fatalf("approval grid has %d cells, want 2×2×2×2 = %d", got, want)
	}
	if got, want := len(cases), len(destructiveAxis)*len(permissionAxis)*len(approvalAxis)*len(interactiveAxis); got != want {
		t.Fatalf("approval grid has %d cells, want the product of every axis %d", got, want)
	}
	if got, want := len(approvalOracle), approvalGridSize; got != want {
		t.Fatalf("approval oracle has %d cells, want %d independently declared cells", got, want)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, ok := approvalOracle[tc.input]
			if !ok {
				t.Fatalf("the literal approval oracle has no entry for %+v", tc.input)
			}
			if got := Decide(tc.input.destructive, tc.input.permitted, tc.input.approved, tc.input.interactive); got != want {
				t.Fatalf("Decide(%+v) = %+v, want literal oracle %+v", tc.input, got, want)
			}
		})
	}
}

func TestApprovalNeverPermitsDestructionWithoutPermission(t *testing.T) {
	for _, interactive := range interactiveAxis {
		t.Run(interactive.name, func(t *testing.T) {
			got := Decide(true, false, true, interactive.value)
			want := Decision{Stopped: StopPermission}
			if got != want {
				t.Fatalf("approved destructive plan without permission = %+v, want %+v", got, want)
			}
		})
	}
}

func TestPermissionWithoutApprovalStopsInANonInteractiveSession(t *testing.T) {
	want := Decision{Stopped: "approval"}
	if got := Decide(false, true, false, false); got != want {
		t.Fatalf("non-interactive plan with permission but no approval = %+v, want %+v", got, want)
	}
}

func TestInteractiveCapabilityIsTheOneFieldApprovalNearTwin(t *testing.T) {
	// This pair differs only in interactive. It proves internal/cli's confirmation capability is
	// an input to Decide, rather than a bool carried but ignored.
	if got := Decide(false, true, false, false); got != (Decision{Stopped: StopApproval}) {
		t.Fatalf("non-interactive unapproved plan = %+v, want approval refusal", got)
	}
	if got := Decide(false, true, false, true); got != (Decision{Proceed: true}) {
		t.Fatalf("interactive confirmation-capable plan = %+v, want proceed", got)
	}
}

func TestNoActionPlanProceedsWithoutFlagsInANonInteractiveSession(t *testing.T) {
	// noActionDecision captures the logical no-action result outside Decide's changed-plan precondition.
	if got := noActionDecision(); got != (Decision{Proceed: true}) {
		t.Fatalf("clean non-interactive plan = %+v, want proceed without either flag", got)
	}
	if got := Decide(false, false, false, false); got == noActionDecision() {
		t.Fatalf("a clean plan must bypass Decide's changed-plan precondition, got %+v", got)
	}
}

type decisionAxis struct {
	name  string
	value bool
}

var (
	destructiveAxis = []decisionAxis{{name: "non_destructive", value: false}, {name: "destructive", value: true}}
	permissionAxis  = []decisionAxis{{name: "permission_absent", value: false}, {name: "permission_given", value: true}}
	approvalAxis    = []decisionAxis{{name: "approval_absent", value: false}, {name: "approval_given", value: true}}
	interactiveAxis = []decisionAxis{{name: "non_interactive", value: false}, {name: "interactive", value: true}}
)

type approvalCase struct {
	name  string
	input approvalInput
}

func crossedApprovalCases() []approvalCase {
	cases := []approvalCase{{}}
	for _, axis := range approvalAxes {
		cases = crossApprovalAxis(cases, axis)
	}
	return cases
}

func crossApprovalAxis(cases []approvalCase, axis approvalGridAxis) []approvalCase {
	next := make([]approvalCase, 0, len(cases)*len(axis.values))
	for _, tc := range cases {
		for _, value := range axis.values {
			name := tc.name + "/" + value.name
			if tc.name == "" {
				name = value.name
			}
			next = append(next, approvalCase{
				name:  name,
				input: tc.input.with(axis.field, value.value),
			})
		}
	}
	return next
}

type approvalField uint8

const (
	destructiveField approvalField = iota
	permittedField
	approvedField
	interactiveField
)

type approvalInput struct{ destructive, permitted, approved, interactive bool }

func (input approvalInput) with(field approvalField, value bool) approvalInput {
	switch field {
	case destructiveField:
		input.destructive = value
	case permittedField:
		input.permitted = value
	case approvedField:
		input.approved = value
	case interactiveField:
		input.interactive = value
	}
	return input
}

type approvalGridAxis struct {
	field  approvalField
	values []decisionAxis
}

var approvalAxes = []approvalGridAxis{
	{field: destructiveField, values: destructiveAxis},
	{field: permittedField, values: permissionAxis},
	{field: approvedField, values: approvalAxis},
	{field: interactiveField, values: interactiveAxis},
}

// approvalOracle is independently declared in destructive, permitted, approved, interactive order.
var approvalOracle = map[approvalInput]Decision{
	{false, false, false, false}: {Stopped: "approval"},
	{false, false, false, true}:  {Proceed: true},
	{false, false, true, false}:  {Proceed: true},
	{false, false, true, true}:   {Proceed: true},
	{false, true, false, false}:  {Stopped: "approval"},
	{false, true, false, true}:   {Proceed: true},
	{false, true, true, false}:   {Proceed: true},
	{false, true, true, true}:    {Proceed: true},
	{true, false, false, false}:  {Stopped: "permission"},
	{true, false, false, true}:   {Stopped: "permission"},
	{true, false, true, false}:   {Stopped: "permission"},
	{true, false, true, true}:    {Stopped: "permission"},
	{true, true, false, false}:   {Stopped: "approval"},
	{true, true, false, true}:    {Proceed: true},
	{true, true, true, false}:    {Proceed: true},
	{true, true, true, true}:     {Proceed: true},
}

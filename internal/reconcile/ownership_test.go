package reconcile

import (
	"fmt"
	"testing"
)

const (
	ownershipDisagreementCount = 4
	ownershipKindCount         = 2
	ownershipGridCellCount     = 8
)

func TestTheOwnershipProofRefusesEachDisagreementOnItsOwnAccount(t *testing.T) {
	assertClosedDisagreementSet(t)
	assertOwnershipGrid(t)
}

func assertClosedDisagreementSet(t *testing.T) {
	t.Helper()
	if got, want := len(disagreements), ownershipDisagreementCount; got != want {
		t.Fatalf("ownership proof declares %d disagreements, want %d", got, want)
	}
	seen := map[Disagreement]bool{}
	for _, disagreement := range disagreements {
		if Disagreement("") == disagreement {
			t.Fatalf("zero Disagreement is %q; an unread proof would look reported", disagreement)
		}
		if seen[disagreement] {
			t.Fatalf("disagreement %q appears twice, so the declared set is not closed", disagreement)
		}
		seen[disagreement] = true
	}
}

func assertOwnershipGrid(t *testing.T) {
	t.Helper()
	if got, want := len(ownershipCases), ownershipDisagreementCount; got != want {
		t.Fatalf("ownership case rows = %d, want %d disagreements", got, want)
	}
	if got, want := len(ownershipKinds), ownershipKindCount; got != want {
		t.Fatalf("ownership grid names %d object kinds, want literal %d", got, want)
	}
	if got, want := len(ownershipCases)*len(ownershipKinds), ownershipGridCellCount; got != want {
		t.Fatalf("ownership grid has %d cells, want literal %d from 4 disagreements × 2 object kinds", got, want)
	}
	assertOwnershipCasesReconcileWithDisagreements(t)
	for _, kind := range ownershipKinds {
		for _, tc := range ownershipCases {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				base := agreeingOwnershipInput(t, kind)
				got := tc.change(t, base)
				assertOnlyNamedProofClauseFails(t, base, got, tc.want)
				ownership := DetermineOwnership(got.recorded, got.found, got.instance)
				if ownership.Disagreement != tc.want || ownership.Owned {
					t.Fatalf("ownership = %+v, want refusal %q", ownership, tc.want)
				}
			})
		}
	}
	assertPositiveControl(t)
}

type ownershipInput struct {
	recorded RegistryRow
	found    CatalogObject
	instance string
}

type ownershipCase struct {
	name   string
	want   Disagreement
	change func(*testing.T, ownershipInput) ownershipInput
}

var ownershipKinds = []string{"trigger", "function"}

var ownershipCases = []ownershipCase{
	// Satisfies the registry row and expected instance; removing the comment alone violates marker presence.
	{name: "marker absent", want: DisagreementMarkerAbsent, change: withoutMarker},
	// Satisfies the registry row and marker pair; changing the expected instance alone violates marker instance.
	{name: "foreign instance", want: DisagreementForeignInstance, change: withForeignInstance},
	// Satisfies the registry row and expected instance; changing the marker pair alone violates pair agreement.
	{name: "marker names another pair", want: DisagreementAnotherPair, change: withAnotherPair},
	// Satisfies the marker and expected instance; removing the registry row alone violates row presence.
	{name: "no registry row", want: DisagreementNoRegistryRow, change: withoutRegistryRow},
}

func assertOwnershipCasesReconcileWithDisagreements(t *testing.T) {
	t.Helper()
	declared := make(map[Disagreement]bool, ownershipDisagreementCount)
	for _, disagreement := range disagreements {
		declared[disagreement] = true
	}
	seen := make(map[Disagreement]string, ownershipDisagreementCount)
	for _, tc := range ownershipCases {
		if !declared[tc.want] {
			t.Errorf("ownership case %q names undeclared disagreement %q", tc.name, tc.want)
		}
		if previous, duplicate := seen[tc.want]; duplicate {
			t.Errorf("ownership cases %q and %q both cover disagreement %q", previous, tc.name, tc.want)
			continue
		}
		seen[tc.want] = tc.name
	}
	for _, disagreement := range disagreements {
		if _, covered := seen[disagreement]; !covered {
			t.Errorf("ownership cases omit declared disagreement %q", disagreement)
		}
	}
}

func agreeingOwnershipInput(t *testing.T, kind string) ownershipInput {
	t.Helper()
	const instance, listener, operation = "alpha", "orders", "insert"
	marker := generatedMarker(t, instance, listener, operation)
	return ownershipInput{
		recorded: RegistryRow{Present: true, Listener: listener, Operation: operation},
		found:    CatalogObject{Kind: kind, Identity: fmt.Sprintf("%s orders", kind), Marker: &marker},
		instance: instance,
	}
}

func withoutMarker(_ *testing.T, input ownershipInput) ownershipInput {
	input.found.Marker = nil
	return input
}

func withForeignInstance(_ *testing.T, input ownershipInput) ownershipInput {
	input.instance = "bravo"
	return input
}

func withAnotherPair(t *testing.T, input ownershipInput) ownershipInput {
	marker := generatedMarker(t, input.instance, "payments", input.recorded.Operation)
	input.found.Marker = &marker
	return input
}

func withoutRegistryRow(_ *testing.T, input ownershipInput) ownershipInput {
	input.recorded.Present = false
	return input
}

func assertOnlyNamedProofClauseFails(t *testing.T, base, got ownershipInput, want Disagreement) {
	t.Helper()
	parsed, readable := parseCatalogMarker(got.found.Marker)
	switch want {
	case DisagreementMarkerAbsent:
		if got.recorded != base.recorded || got.instance != base.instance || got.found.Kind != base.found.Kind || got.found.Identity != base.found.Identity || readable {
			t.Fatal("marker-absent case changed proof evidence besides the comment")
		}
	case DisagreementForeignInstance:
		if got.recorded != base.recorded || got.found != base.found || got.instance == base.instance || !readable || parsed.Instance != base.instance || parsed.Listener != got.recorded.Listener || parsed.Operation != got.recorded.Operation {
			t.Fatal("foreign-instance case changed proof evidence besides expected instance")
		}
	case DisagreementAnotherPair:
		if got.recorded != base.recorded || got.instance != base.instance || !readable || parsed.Instance != got.instance || parsed.Listener == got.recorded.Listener && parsed.Operation == got.recorded.Operation {
			t.Fatal("another-pair case changed proof evidence besides marker pair")
		}
	case DisagreementNoRegistryRow:
		if got.instance != base.instance || got.found != base.found || !readable || parsed.Instance != got.instance || parsed.Listener != got.recorded.Listener || parsed.Operation != got.recorded.Operation || got.recorded.Present || got.recorded.Listener != base.recorded.Listener || got.recorded.Operation != base.recorded.Operation {
			t.Fatal("no-registry-row case changed proof evidence besides row presence")
		}
	default:
		t.Fatalf("test case names unknown disagreement %q", want)
	}
}

func assertPositiveControl(t *testing.T) {
	t.Helper()
	base := agreeingOwnershipInput(t, "trigger")
	if got := DetermineOwnership(base.recorded, base.found, base.instance); !got.Owned || got.Disagreement != "" || got.Found != *base.found.Marker {
		t.Fatalf("all-agreeing ownership = %+v, want owned with zero disagreement", got)
	}
	foreign, absent := withForeignInstance(t, base), withoutMarker(t, base)
	if foreign.recorded != base.recorded || foreign.found != base.found || foreign.instance == base.instance || absent.recorded != base.recorded || absent.instance != base.instance || absent.found.Marker != nil {
		t.Fatal("negative twins differ from their positive control in more than one field")
	}
}

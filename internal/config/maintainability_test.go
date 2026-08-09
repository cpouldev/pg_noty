package config

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
)

type visibleDiagnostic struct {
	File      string
	Line, Col int
	Path      string
	Msg, Hint string
}

type ruleIndependenceSnapshot struct {
	rendered string
	deduped  []visibleDiagnostic
	ordered  []visibleDiagnostic
}

func TestRuleIDPermutationCannotChangeRenderedOrOrderedDiagnostics(t *testing.T) {
	source := []byte("alpha: 1\nbeta: 2\n")
	assertRuleIndependentProductionSurfaces(t, source)

	baseline := Errors{
		{Rule: R1, File: "rule.yaml", Line: 2, Col: 7, Path: "beta",
			Msg: "must satisfy the beta constraint"},
		{Rule: R2, File: "rule.yaml", Line: 1, Col: 8, Path: "alpha.z",
			Msg: "must satisfy the alpha constraint"},
		{Rule: R3, File: "rule.yaml", Line: 1, Col: 8, Path: "alpha.a",
			Msg: "must satisfy the alpha constraint"},
	}
	permuted := append(Errors(nil), baseline...)
	permuted[0].Rule, permuted[1].Rule, permuted[2].Rule =
		permuted[2].Rule, permuted[0].Rule, permuted[1].Rule

	before := observeRuleIndependence(baseline, source)
	after := observeRuleIndependence(permuted, source)
	if issues := ruleIndependenceIssues(before, after); len(issues) != 0 {
		t.Fatalf("RuleID permutation changed maintainability surfaces: %v", issues)
	}
}

func assertRuleIndependentProductionSurfaces(t *testing.T, source []byte) {
	t.Helper()
	first := Error{
		Rule: R1, File: "rule.yaml", Line: 1, Col: 8, Path: "alpha",
		Msg: "must satisfy the alpha constraint",
	}
	second := first
	second.Rule = R2

	t.Run("renderer", func(t *testing.T) {
		if want, got := (Errors{first}).Render(source), (Errors{second}).Render(source); want != got {
			t.Fatal("renderer changed when only RuleID changed")
		}
	})
	t.Run("dedupe key", func(t *testing.T) {
		if got := (Errors{first, second}).deduped(); len(got) != 1 {
			t.Fatalf("RuleID-only difference produced %d complaints, want 1", len(got))
		}
	})
	t.Run("sort key", func(t *testing.T) {
		if compareByPosition(first, second) != 0 || compareByPosition(second, first) != 0 {
			t.Fatal("sort key changed when only RuleID changed")
		}
	})
}

func TestRuleIndependenceGateRejectsSyntheticCoupling(t *testing.T) {
	baseline := ruleIndependenceSnapshot{
		rendered: "stable",
		deduped:  []visibleDiagnostic{{File: "a.yaml", Line: 1, Col: 1, Path: "a", Msg: "constraint"}},
		ordered:  []visibleDiagnostic{{File: "a.yaml", Line: 1, Col: 1, Path: "a", Msg: "constraint"}},
	}
	tests := []struct {
		name   string
		mutate func(ruleIndependenceSnapshot) ruleIndependenceSnapshot
	}{
		{"renderer uses RuleID", func(got ruleIndependenceSnapshot) ruleIndependenceSnapshot {
			got.rendered += " R1"
			return got
		}},
		{"dedupe key uses RuleID", func(got ruleIndependenceSnapshot) ruleIndependenceSnapshot {
			got.deduped = append(got.deduped, got.deduped[0])
			return got
		}},
		{"sort key uses RuleID", func(got ruleIndependenceSnapshot) ruleIndependenceSnapshot {
			got.ordered = append([]visibleDiagnostic{{File: "z.yaml"}}, got.ordered...)
			return got
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if issues := ruleIndependenceIssues(baseline, tc.mutate(baseline)); len(issues) == 0 {
				t.Fatal("synthetic RuleID coupling passed the maintainability gate")
			}
		})
	}
}

func observeRuleIndependence(diags Errors, source []byte) ruleIndependenceSnapshot {
	deduped := visibleDiagnostics(diags.deduped())
	slices.SortFunc(deduped, compareVisibleDiagnostics)
	ordered := diags.normalized()
	return ruleIndependenceSnapshot{
		rendered: ordered.Render(source),
		deduped:  deduped,
		ordered:  visibleDiagnostics(ordered),
	}
}

func ruleIndependenceIssues(want, got ruleIndependenceSnapshot) []string {
	var issues []string
	if want.rendered != got.rendered {
		issues = append(issues, "renderer bytes changed")
	}
	if !reflect.DeepEqual(want.deduped, got.deduped) {
		issues = append(issues, "dedupe result changed")
	}
	if !reflect.DeepEqual(want.ordered, got.ordered) {
		issues = append(issues, "ordered result changed")
	}
	return issues
}

func visibleDiagnostics(diags Errors) []visibleDiagnostic {
	visible := make([]visibleDiagnostic, len(diags))
	for i, diag := range diags {
		visible[i] = visibleDiagnostic{
			File: diag.File, Line: diag.Line, Col: diag.Col, Path: diag.Path,
			Msg: diag.Msg, Hint: diag.Hint,
		}
	}
	return visible
}

func compareVisibleDiagnostics(a, b visibleDiagnostic) int {
	return compareByPosition(
		Error{File: a.File, Line: a.Line, Col: a.Col, Path: a.Path, Msg: a.Msg},
		Error{File: b.File, Line: b.Line, Col: b.Col, Path: b.Path, Msg: b.Msg},
	)
}

func (snapshot ruleIndependenceSnapshot) String() string {
	return fmt.Sprintf("%q %v %v", snapshot.rendered, snapshot.deduped, snapshot.ordered)
}

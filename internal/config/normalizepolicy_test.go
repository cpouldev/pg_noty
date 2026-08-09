package config

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// Stage E's error policy: accumulate across the whole document, then stop (load.go).
//
// The three claims here are what "accumulate then stop" is made of at this stage -- every alias
// a document cannot resolve is named in one run, a refused node keeps what its author wrote, and
// the run that stops carries no warnings and no configuration.
//
// The file is named for the stage's own function rather than for its letter, so that it cannot be
// misread as stagedpolicy_test.go, which makes the same three claims about a different stage in a
// package whose files are flat and whose other stage-E files are named the same way
// (normalize.go, anchor.go, mergekey.go, listsugar.go).

// TestTwoIndependentAliasFaultsAreBothReportedBeforeTheRunStops is the accumulate half, which is
// what tells this policy apart from fatal-single: one run names both mistakes rather than the
// first one and a promise to look again.
func TestTwoIndependentAliasFaultsAreBothReportedBeforeTheRunStops(t *testing.T) {
	_, _, diags := stageE(t, "first: *nowhere\nsecond: *elsewhere\n", nil)

	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics %q, want one per undefined alias", len(diags), messagesOf(diags))
	}
	if diags[0].Line == diags[1].Line {
		t.Errorf("both diagnostics are on line %d, so one of the two faults went unreported", diags[0].Line)
	}
}

// stoppedAtStageE is a run the stage refused, taken through the entry point a caller uses. The
// two things such a run must not carry are asserted separately, because they come from different
// stages -- a configuration from stage I, warnings from stages H and I -- so a failure names the
// stage that started running when it should not have.
func stoppedAtStageE(t *testing.T) (*Config, Warnings) {
	t.Helper()

	cfg, warnings, errs := Parse([]byte("use: *nowhere\n"), interpolationFixture, MapEnv(nil))
	if len(errs) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one refusing the alias", len(errs), messagesOf(errs))
	}
	if errs[0].Rule != RuleNormalize {
		t.Errorf("Rule = %q, want every diagnostic of this run to come from stage E", errs[0].Rule)
	}
	return cfg, warnings
}

// TestStageEStopsTheRunWithNoConfiguration is the stop half a caller sees first: a document
// refused here is diagnostics and nothing else (AC #23 case 3's shape, one stage on).
func TestStageEStopsTheRunWithNoConfiguration(t *testing.T) {
	if cfg, _ := stoppedAtStageE(t); cfg != nil {
		t.Error("a run that stopped at stage E returned a configuration")
	}
}

// TestStageEStopsTheRunWithNoWarnings is the other half, and it is not implied by the first: W1
// and W2 are computed in stages H and I, which this run never reaches, so a warning here would
// mean a later stage ran over a document still holding an unresolved alias.
func TestStageEStopsTheRunWithNoWarnings(t *testing.T) {
	if _, warnings := stoppedAtStageE(t); len(warnings) != 0 {
		t.Errorf("got %d warnings, want none: W1 and W2 are computed in stages H and I", len(warnings))
	}
}

// TestAnUnresolvedAliasKeepsTheTextItWasWrittenWith is the counterpart of stage D's
// Implementation Note 9. The run stops here, so nothing reads the value, and leaving it alone
// means the only thing a later reader of this tree could find in that position is the alias its
// author actually wrote.
func TestAnUnresolvedAliasKeepsTheTextItWasWrittenWith(t *testing.T) {
	_, root, diags := stageE(t, "use: *nowhere\n", nil)

	if len(diags) == 0 {
		t.Fatal("an undefined alias was accepted")
	}
	if _, stillAnAlias := valueOfEntry(root, 0).(*ast.AliasNode); !stillAnAlias {
		t.Errorf("the refused alias became %T; a refused node keeps what its author wrote", valueOfEntry(root, 0))
	}
}

// TestAnUnresolvedAliasInAnOperationsListIsRefusedOnlyOnce is ADR-3's "at most one diagnostic
// per offending token" where two of this stage's own answers meet. The element is refused as an
// alias naming no anchor; the fold then meets the same token and must not add a second, and
// differently worded, complaint about a key it cannot read.
func TestAnUnresolvedAliasInAnOperationsListIsRefusedOnlyOnce(t *testing.T) {
	// Line 2 is `  - *nowhere`, so the alias begins at rune 5.
	_, _, diags := stageE(t, "operations:\n  - *nowhere\n", nil)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one about the alias", len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], undefinedAlias, 2, 5)
}

// TestAnAnchorCannotNameItselfBecauseItIsRecordedOnlyOnceItsValueIsReduced is why the walk
// terminates without a set of visited nodes. An anchor joins the table after its value has been
// reduced, so an alias inside that value finds no anchor of the name and is refused rather than
// followed forever.
//
// What it is refused *with* is the second half, and it is a different mistake from naming an
// anchor nobody declared: this author did define the anchor with `&name` above the alias, so
// the undefined-alias hint would send them to do what they had already done.
func TestAnAnchorCannotNameItselfBecauseItIsRecordedOnlyOnceItsValueIsReduced(t *testing.T) {
	// Line 2 is `  inner: *loop`, so the alias begins at rune 10.
	_, _, diags := stageE(t, "loop: &loop\n  inner: *loop\n", nil)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one refusing a self-reference", len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], selfReferentialAnchor, 2, 10)
}

// TestAnAliasNamingAnAnchorDeclaredNowhereIsTheOtherRefusal is the other side of that guard.
// The two conditions reach the same empty answer from the anchor table -- the name is not in it
// -- and must not reach the same diagnostic, so the case that must keep the undefined-alias
// wording is asserted beside the one that must not have it.
func TestAnAliasNamingAnAnchorDeclaredNowhereIsTheOtherRefusal(t *testing.T) {
	// Line 2 is `  inner: *elsewhere`, the same position as the self-reference above, so the two
	// differ in nothing but the anchor's name.
	_, _, diags := stageE(t, "loop: &loop\n  inner: *elsewhere\n", nil)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one refusing an undefined alias", len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], undefinedAlias, 2, 10)
}

// TestAMergeSourceNamingTheAnchorItIsWrittenInsideIsRefusedAsASelfReference carries the same
// distinction to the other position that resolves a name. Both callers of the anchor table ask
// one question, so both must get the answer that fits the input rather than the answer the first
// caller happened to need.
func TestAMergeSourceNamingTheAnchorItIsWrittenInsideIsRefusedAsASelfReference(t *testing.T) {
	// Line 2 is `  <<: *loop`, so the alias begins at rune 7.
	_, _, diags := stageE(t, "loop: &loop\n  <<: *loop\n", nil)

	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics %q, want the one refusing a self-reference", len(diags), messagesOf(diags))
	}
	assertDiagnostic(t, diags[0], selfReferentialAnchor, 2, 7)
}

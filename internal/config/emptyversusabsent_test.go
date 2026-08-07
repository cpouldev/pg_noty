package config

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// AC #31's distinction, which has a file of its own because of what it costs to lose: in
// internal/reconcile an explicitly empty `listeners` list is a valid no-op and an absent one
// would drop every trigger this instance owns. The two must never be conflated in either
// direction.

// TestAnEmptyListenersListIsValidAndAnAbsentOneIsNot is AC #31, and the reason it has a test of
// its own is written into it: in internal/reconcile the two are the difference between a valid
// no-op and dropping every trigger this instance owns. A simplification that collapsed them --
// treating an empty list as absent, or an absent key as an empty list -- would be a silent
// data-loss change, so it has to fail here rather than be caught in review.
func TestAnEmptyListenersListIsValidAndAnAbsentOneIsNot(t *testing.T) {
	t.Run("an explicitly empty list", func(t *testing.T) {
		diags, decodable := stageF(t, rootWithEverything)

		if len(diags) != 0 {
			t.Errorf("%d diagnostics for an explicitly empty list: %q", len(diags), messagesOf(diags))
		}
		if !decodable {
			t.Error("an explicitly empty list left the document undecodable")
		}
	})

	t.Run("no listeners key at all", func(t *testing.T) {
		diags, _ := stageF(t, "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\n")

		if len(diags) != 1 {
			t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
		}
		if diags[0].Rule != R22 {
			t.Errorf("Rule = %q, want R22", diags[0].Rule)
		}
	})
}

// TestAnEmptyListenersListHoldsNoListeners is the value half of AC #31 as far as this step can
// reach it: the list survives stage F holding nothing, which is what Step 10 will decode into zero
// listeners.
//
// **Step 10 must extend this to read Config.Listeners back through Parse**. `Parse` returns no
// configuration until stage I is wired, so "yields zero listeners" cannot be asserted end-to-end
// here; what can be asserted is that nothing between the parser and this stage invented one.
func TestAnEmptyListenersListHoldsNoListeners(t *testing.T) {
	_, root, _ := stageE(t, rootWithEverything, corpusVariables)

	listeners, isSequence := nodeIn(t, root, "$.listeners").(*ast.SequenceNode)
	if !isSequence {
		t.Fatalf("listeners is %T, want an empty list", nodeIn(t, root, "$.listeners"))
	}
	if len(listeners.Values) != 0 {
		t.Errorf("the empty list holds %d listeners, want none", len(listeners.Values))
	}

	cfg, warnings, errs := Parse([]byte(rootWithEverything), "empty-listeners.yaml", corpusEnvironment())
	if len(errs) != 0 || len(warnings) != 0 || cfg == nil {
		t.Fatalf("Parse returned config=%v warnings=%+v errors=%+v", cfg, warnings, errs)
	}
	if len(cfg.Listeners) != 0 {
		t.Errorf("Config.Listeners holds %d listeners, want none", len(cfg.Listeners))
	}
}

// TestEveryRequiredKeyBelowAScopeIsReportedByThatScope closes the anchoring over the table rather
// than over the seven cases above. An eighth required key added beneath either scope has to be
// reported by it, and a required key beneath a *third* level would today be reported by whichever
// scope encloses it -- which is correct, and is what this asserts rather than assumes.
func TestEveryRequiredKeyBelowAScopeIsReportedByThatScope(t *testing.T) {
	fromRoot := pathsIn(requiredKeysAbsentFrom(schemaLevels[levelRoot], nil, ""))
	fromListener := pathsIn(requiredKeysAbsentFrom(schemaLevels[levelListener], nil, ""))

	wantRoot := []string{"version", "database.url", "listeners"}
	wantListener := []string{"name", "table", operationsKey, "destination.url"}

	if !equalStrings(fromRoot, wantRoot) {
		t.Errorf("the root scope answers for %v, want %v", fromRoot, wantRoot)
	}
	if !equalStrings(fromListener, wantListener) {
		t.Errorf("the listener scope answers for %v, want %v", fromListener, wantListener)
	}
	if len(fromRoot)+len(fromListener) != len(requiredKeyRules) {
		t.Errorf("the two scopes answer for %d keys between them, want the %d the contract requires",
			len(fromRoot)+len(fromListener), len(requiredKeyRules))
	}
}

// pathsIn is the paths of a scope's missing keys, in the order the scope reports them.
func pathsIn(missing []missingKey) []string {
	paths := make([]string, 0, len(missing))
	for _, key := range missing {
		paths = append(paths, key.path)
	}
	return paths
}

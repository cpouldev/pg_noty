package reconcile

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

const (
	emptyListenersFixture  = "../config/testdata/valid/R22_ok_listeners_empty_list.yaml"
	absentListenersFixture = "../config/testdata/invalid/R22_listeners_absent.yaml"
)

// scopeShape is what a listenerScope's fixture actually is, measured through the production walk
// rather than read back off the row's own outcome list: how many of its pairs the run leaves alone,
// and how many listeners keep their registry row.
type scopeShape struct{ surviving, retainedListeners int }

func measuredScopeShape(t *testing.T, scope listenerScope) scopeShape {
	t.Helper()
	sources, shape, retained := collectPairSources(scope.input), scopeShape{}, map[string]bool{}
	for _, outcome := range scope.outcomes {
		got := classifyPair(preparedPairSource(scope.input, sources, outcome.pair))
		if got.Action == nil {
			shape.surviving++
			continue
		}
		retained[outcome.pair.Listener] = retained[outcome.pair.Listener] || got.RetainListener
	}
	for _, keeps := range retained {
		if keeps {
			shape.retainedListeners++
		}
	}
	return shape
}

// TestTheAdjacentListenerScopesDifferInThePropertyTheyAreNamedFor is what stops criteria 16 and 18
// being re-fixtured as their neighbours. Each pair below is adjacent -- one property apart -- and the
// property is measured from the fixture rather than taken from the row's name.
func TestTheAdjacentListenerScopesDifferInThePropertyTheyAreNamedFor(t *testing.T) {
	removedOperation := measuredScopeShape(t, removedOperationScope(t))
	noOperations := measuredScopeShape(t, noOperationsRetainedScope(t))
	if removedOperation.surviving != 1 || noOperations.surviving != 0 {
		t.Fatalf(
			"criterion 15 leaves %d of its listener's pairs and criterion 16 leaves %d; 16 is the "+
				"row where none remain, so equal counts mean the two rows share one input",
			removedOperation.surviving, noOperations.surviving,
		)
	}
	if noOperations.retainedListeners != 1 {
		t.Fatalf(
			"criterion 16 retained %d listeners though none of its pairs remain, want 1; that is "+
				"the answer a classifier keyed on \"no triggers remain\" gets wrong", noOperations.retainedListeners,
		)
	}

	listenerRemoved := measuredScopeShape(t, listenerRemovedScope(t))
	emptyList := measuredScopeShape(t, emptyListenerListScope(t))
	if listenerRemoved.surviving != 1 || emptyList.surviving != 0 {
		t.Fatalf(
			"criterion 17 leaves %d pairs and criterion 18 leaves %d; 18 removes everything this "+
				"instance owns, so equal counts mean the two rows share one input",
			listenerRemoved.surviving, emptyList.surviving,
		)
	}
	if listenerRemoved.retainedListeners != 0 || emptyList.retainedListeners != 0 {
		t.Fatalf(
			"criteria 17 and 18 retained %d and %d listeners, want none; a listener removed from "+
				"the configuration keeps no registry row",
			listenerRemoved.retainedListeners,
			emptyList.retainedListeners,
		)
	}
}

// TestAnEmptyListenersListIsAConfigurationAndAnAbsentKeyIsNot is the second half of the empty
// listener list case, as far as this package can carry it. The first half -- an explicitly empty
// list plans the removal of everything this instance owns -- is emptyListenerListScope. The second
// is that an absent listeners key produces a configuration error and no plan at all, and that the
// two are answered by different code paths rather than by one.
//
// What is asserted here is the crossing, read from internal/config's own R22 corpus rather than from
// a document written here: the empty list parses to a configuration declaring no listener, which is
// exactly what a diffInput with no desired listener is built from, while the absent key parses to no
// configuration, so no diffInput can be built from it at all. That Plan therefore returns no plan
// belongs to reconcile.go, which is where a config.Config first becomes a diffInput.
func TestAnEmptyListenersListIsAConfigurationAndAnAbsentKeyIsNot(t *testing.T) {
	empty, _, diagnostics := config.Parse(sourceBytes(t, emptyListenersFixture), "listeners.yaml", config.MapEnv(nil))
	if empty == nil || len(diagnostics) != 0 {
		t.Fatalf("Parse(explicitly empty list) = %#v, %#v; want a configuration and no diagnostic", empty, diagnostics)
	}
	if len(empty.Listeners) != 0 {
		t.Fatalf("the explicitly empty list parsed to %d listeners, want none", len(empty.Listeners))
	}

	absent, _, diagnostics := config.Parse(sourceBytes(t, absentListenersFixture), "listeners.yaml", config.MapEnv(nil))
	if absent != nil || len(diagnostics) != 1 {
		t.Fatalf(
			"Parse(absent listeners key) = %#v, %#v; want no configuration and one diagnostic",
			absent,
			diagnostics,
		)
	}
	if diagnostics[0].Rule != config.R22 {
		t.Fatalf(
			"the absent listeners key reported %s, want %s; R22 is the rule that keeps an absent "+
				"key from reaching this package as an empty one", diagnostics[0].Rule, config.R22,
		)
	}
}

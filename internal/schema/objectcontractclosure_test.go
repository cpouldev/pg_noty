package schema

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

// The pins that keep the object contract closed, and the synthetic seventh table that proves they
// can bite. A pin nothing ever fails is a pin that may already have stopped covering the set it
// names, so each one here is driven with an input built to break it.

// theSeventhTable is a table the contract does not list. It stands in for the growth this step's
// inventories have to notice: a table added to the schema without being added to the contract.
const theSeventhTable = "audit_log"

// widenedContract is the shipped contract plus one column of a table nothing pins, which is the
// exact shape a seventh table arrives in.
func widenedContract() []contractColumn {
	return append(slices.Clone(theObjectContract),
		contractColumn{table: theSeventhTable, name: "at", sqlType: "timestamptz", notNull: true})
}

// columnCountIssues reports how one column contract diverges from the per-table sizes pinned for
// it, in both directions: a table carrying columns that nothing pins, and a pin whose count the
// contract no longer matches. The shipped assertion and the seventh-table case both run through it,
// so the negative case drives the assertion's own body rather than a second expression of the same
// idea.
func columnCountIssues(contract []contractColumn, pinned map[string]int) []string {
	counted := map[string]int{}
	for _, column := range contract {
		counted[column.table]++
	}

	var issues []string
	for _, table := range slices.Sorted(maps.Keys(counted)) {
		want, isPinned := pinned[table]
		switch {
		case !isPinned:
			issues = append(issues, fmt.Sprintf("%s carries %d columns and no pinned count, so its "+
				"inventory can shrink unnoticed", table, counted[table]))
		case counted[table] != want:
			issues = append(issues, fmt.Sprintf("%s carries %d columns and %d are pinned",
				table, counted[table], want))
		}
	}
	for _, table := range slices.Sorted(maps.Keys(pinned)) {
		if _, carries := counted[table]; !carries {
			issues = append(issues, table+" has a pinned column count and the contract lists no "+
				"column for it")
		}
	}
	return issues
}

func TestEveryContractTableCarriesExactlyItsPinnedNumberOfColumns(t *testing.T) {
	for _, issue := range columnCountIssues(theObjectContract, theContractColumnCounts) {
		t.Error(issue)
	}
}

// TestASeventhTableFailsTheTableSetAndTheColumnCountPins is the negative case criterion 3 asks for.
// Both pins are driven, because they fail for different reasons and only one of them counts
// tables: a set derived from the contract grows silently, and a per-table count has no entry at all
// for the newcomer.
func TestASeventhTableFailsTheTableSetAndTheColumnCountPins(t *testing.T) {
	widened := widenedContract()

	if got := len(contractTablesOf(widened)); got != len(theContractTables)+1 {
		t.Errorf("a seventh table left the derived table set at %d entries, want %d; the set the "+
			"catalog inventory ranges over would not have grown with the schema",
			got, len(theContractTables)+1)
	}

	issues := columnCountIssues(widened, theContractColumnCounts)
	if len(issues) != 1 || !strings.Contains(issues[0], theSeventhTable) {
		t.Errorf("a seventh table produced %v, want exactly one issue naming %s; a pin that cannot "+
			"fail here is one that has stopped covering the contract", issues, theSeventhTable)
	}
}

// TestAnEighthColumnFailsItsTablesOwnPin is the same closure one level down. The table set is
// unchanged by an eighth column, so only the per-table count can report it -- which is why the
// counts are pinned per table rather than as one total.
func TestAnEighthColumnFailsItsTablesOwnPin(t *testing.T) {
	widened := append(slices.Clone(theObjectContract),
		contractColumn{table: TableEvents, name: "region", sqlType: "text", notNull: true})

	issues := columnCountIssues(widened, theContractColumnCounts)
	want := fmt.Sprintf("%s carries %d columns and %d are pinned",
		TableEvents, theContractColumnCounts[TableEvents]+1, theContractColumnCounts[TableEvents])
	if !slices.Contains(issues, want) {
		t.Errorf("an eighth column on %s produced %v, want an issue reading %q",
			TableEvents, issues, want)
	}
}

// TestTheStatusVocabularyRunsInBothDirections is criterion 4's own closure, asserted before a container
// is involved: an accept-only set would satisfy every row of the tagged assertion that ranges over it,
// whatever that assertion did.
func TestTheStatusVocabularyRunsInBothDirections(t *testing.T) {
	accepted := 0
	for _, status := range theStatusVocabulary {
		if status.accepted {
			accepted++
		}
		if status.why == "" {
			t.Errorf("%q records no reason for its verdict", status.value)
		}
	}

	if rejected := len(theStatusVocabulary) - accepted; accepted != 3 || rejected != 3 {
		t.Errorf("the vocabulary accepts %d values and rejects %d, want three of each; the three "+
			"rejected are the decisive ones, since a CHECK still admitting delivered and failed is "+
			"indistinguishable from a correct one under accept-only testing", accepted, rejected)
	}
}

// TestADroppedPinIsReportedRatherThanSilentlyHonoured is the other direction of the same helper: a
// contract table whose pin was removed reads as pinned to nothing, and the size assertion in
// objectcontract_test.go would then be repaired by adding an entry for a table that does not exist.
func TestADroppedPinIsReportedRatherThanSilentlyHonoured(t *testing.T) {
	shortened := maps.Clone(theContractColumnCounts)
	delete(shortened, TableDeliveries)

	issues := columnCountIssues(theObjectContract, shortened)
	if len(issues) != 1 || !strings.Contains(issues[0], TableDeliveries) {
		t.Errorf("dropping %s's pin produced %v, want exactly one issue naming it",
			TableDeliveries, issues)
	}
}

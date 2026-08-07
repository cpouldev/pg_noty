package schema

import (
	"maps"
	"slices"
	"testing"
)

// This file is the container-free half of the object contract the packages above this one compile
// against: the declared sets every catalog inventory in objectcontract*_integration_test.go ranges
// over, each with its size pinned. An inventory written as a list of ifs at a call site passes
// identically today and stops covering the seventh table silently, which is why every set here is a
// value and every size is an assertion.
//
// Each set is a transcription of an external contract -- the object contract the packages above
// this one compile against, and the status vocabulary -- and never a derivation from the
// DDL it checks. The two the package already declares, theObjectContract
// (migrationcontract_test.go) and theDeclaredIndexes (migrationindex_test.go), are read rather than
// re-listed: a local re-listing of the contract is the copy most likely to drift.

// theContractTables is the six tables in the contract table's own order, derived from the column
// contract rather than written out again.
var theContractTables = contractTablesOf(theObjectContract)

// contractTablesOf is the distinct tables one column contract names, in first-appearance order. It
// takes the contract as an argument so the synthetic seventh table in objectcontractclosure_test.go
// can drive this very function rather than a second expression of it.
func contractTablesOf(contract []contractColumn) []string {
	var named []string
	for _, column := range contract {
		if !slices.Contains(named, column.table) {
			named = append(named, column.table)
		}
	}
	return named
}

// theContractColumnCounts is how many columns the contract table gives each table. Without a
// per-table pin a column dropped from one table would leave that table's inventory quietly smaller,
// and the 38-row total could be repaired by a column added to another.
var theContractColumnCounts = map[string]int{
	TableSchemaVersion:    3,
	TableListeners:        7,
	TableListenerTriggers: 5,
	TableEvents:           7,
	TableEventQueue:       9,
	TableDeliveries:       7,
}

// theContractKeys is the `Key` column of the contract table, one entry per table. The event log's
// composite key is forced by the server rather than chosen -- M1 measured both PRIMARY KEY (id) and
// UNIQUE (id) refused on a partitioned table -- so that entry is evidence about the server and
// about neither AC 39 nor the queue's shape.
var theContractKeys = map[string][]string{
	TableSchemaVersion:    {"version"},
	TableListeners:        {"name"},
	TableListenerTriggers: {"listener", "operation"},
	TableEvents:           {"id", "occurred_at"},
	TableEventQueue:       {"event_id"},
	TableDeliveries:       {"event_id", "attempt"},
}

// theContractChecks is how many CHECK constraints the contract puts on each table: one on the
// queue's status and none anywhere else. deliveries carries none deliberately -- response_snippet's
// 2 KiB cap is a column comment, because a rejected insert would abort internal/delivery's record
// transaction, leave the queue row delivering until lease reclaim, and turn a truncation bug into
// an infinite redelivery loop. The zeroes are the property, so they are written rather than implied
// by a table's absence from the map.
var theContractChecks = map[string]int{
	TableSchemaVersion:    0,
	TableListeners:        0,
	TableListenerTriggers: 0,
	TableEvents:           0,
	TableEventQueue:       1,
	TableDeliveries:       0,
}

// statusCase is one value of the queue's status vocabulary and the verdict its CHECK must give it.
type statusCase struct {
	value    string
	accepted bool
	// why records what the verdict rests on, so a later widening meets the reason rather than an
	// unexplained row.
	why string
}

// theStatusVocabulary is criterion 4 in both directions. delivered and failed are named rows rather
// than members of "anything else": both were deliberately retired from the original five-state
// design, so a CHECK still admitting them is indistinguishable from a correct one under accept-only
// testing. The unlisted string is a third rejected row because a five-state CHECK would reject it
// while admitting the two retired states.
var theStatusVocabulary = []statusCase{
	{value: "pending", accepted: true, why: "the state an enqueued event waits in"},
	{value: "delivering", accepted: true, why: "the transient state a lease holds a row in"},
	{value: "dead", accepted: true, why: "the decision to permit the event's eventual deletion"},
	{value: "delivered", accepted: false,
		why: "retired: a delivered event is the queue row's absence and not a fourth state"},
	{value: "failed", accepted: false,
		why: "retired: a failed attempt is pending with attempts above zero"},
	{value: "requeued", accepted: false,
		why: "an arbitrary unlisted string, which a five-state CHECK would reject as well"},
}

// theEventLogAbsences are the three columns criterion 5 keeps off the partitioned table. Each
// carries the reason it matters, so a later "improvement" adding one meets that reason rather than
// an unexplained absence.
var theEventLogAbsences = []struct{ column, why string }{
	{column: "status", why: "the mutable delivery state lives in the queue, which is what keeps " +
		"the claim path off this partitioned table"},
	{column: "attempts", why: "the event log is append-only, and a retry counter here would have " +
		"a redelivery rewrite an event row"},
	{column: "next_attempt_at", why: "the claim query's range scan is served by the queue's " +
		"partial index, and a column here would invite that scan back onto the partitioned table"},
}

// foreignKey is one declared foreign key as the catalog reports it structurally: the referencing
// columns in key order, the relation referenced, and its columns in key order.
type foreignKey struct {
	columns    []string
	references string
	referenced []string
}

// theRatifiedCompositeKey is AC 39's ratified answer (ADR-9, Accepted), transcribed from the
// contract table. It is asserted separately from event_queue.occurred_at, because the column
// without the key composes nothing, and separately from the event log's composite primary key,
// because M1 forces that key under every reading of AC 39 -- neither is evidence for the other.
var theRatifiedCompositeKey = foreignKey{
	columns:    []string{"event_id", "occurred_at"},
	references: TableEvents,
	referenced: []string{"id", "occurred_at"},
}

// theEventLogPartitionClause is the contract table's own partitioning clause. The catalog reports
// it without the leading keywords, so the tagged half derives its expectation from this literal
// rather than transcribing what a run answered.
const theEventLogPartitionClause = "PARTITION BY RANGE (occurred_at)"

// TestEveryDeclaredSetThisStepsInventoriesRangeOverHasItsSizePinned is the step's gate. Every
// inventory in the tagged half ranges over one of these sets, so a set that can shrink is an
// inventory that can stop covering an object with nothing red.
//
// theObjectContract's 38 columns and theDeclaredIndexes' four entries are pinned where they are
// declared, by TestTheObjectContractSizeIsPinned and
// TestTheFourDeclaredIndexesAreExactlyWhatTheContractNames, and are deliberately not pinned a
// second time here.
func TestEveryDeclaredSetThisStepsInventoriesRangeOverHasItsSizePinned(t *testing.T) {
	for _, tc := range []struct {
		named string
		held  int
		want  int
	}{
		{named: "the contract's tables", held: len(theContractTables), want: 6},
		{named: "the pinned per-table column counts", held: len(theContractColumnCounts), want: 6},
		{named: "the contract's primary keys", held: len(theContractKeys), want: 6},
		{named: "the contract's CHECK counts", held: len(theContractChecks), want: 6},
		{named: "the status vocabulary", held: len(theStatusVocabulary), want: 6},
		{named: "the event log's asserted absences", held: len(theEventLogAbsences), want: 3},
	} {
		if tc.held != tc.want {
			t.Errorf("%s holds %d entries, want %d; update this count with the set, or an entry "+
				"dropped from it stops being asserted anywhere", tc.named, tc.held, tc.want)
		}
	}
}

// TestEveryPerTablePinIsKeyedOnTheContractsOwnTables closes the two maps over the tables they
// describe. A pin keyed on a name no table carries fires for nothing, and the size assertion above
// cannot tell that from a correct one -- six entries is six entries either way.
func TestEveryPerTablePinIsKeyedOnTheContractsOwnTables(t *testing.T) {
	want := slices.Sorted(slices.Values(theContractTables))

	for _, named := range []struct {
		what  string
		keyed []string
	}{
		{what: "theContractKeys", keyed: slices.Sorted(maps.Keys(theContractKeys))},
		{what: "theContractChecks", keyed: slices.Sorted(maps.Keys(theContractChecks))},
	} {
		if !slices.Equal(named.keyed, want) {
			t.Errorf("%s is keyed on %v, want the contract's own tables %v",
				named.what, named.keyed, want)
		}
	}
}

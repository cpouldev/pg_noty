package schema

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// theObjectContract is the object contract the packages above this one compile against, written
// out as literals. These types and nullabilities are dictated by the contract rather than computed
// by anything here, so they are pinned against the table and never derived from the DDL they
// check. A column absent or mistyped is not caught in this package at all otherwise -- it is caught
// in a package above this one, as a column that does not exist.
type contractColumn struct {
	table   string
	name    string
	sqlType string
	notNull bool
}

var theObjectContract = []contractColumn{
	{table: TableSchemaVersion, name: "version", sqlType: "int", notNull: true},
	{table: TableSchemaVersion, name: "checksum", sqlType: "text", notNull: true},
	{table: TableSchemaVersion, name: "applied_at", sqlType: "timestamptz", notNull: true},

	{table: TableListeners, name: "name", sqlType: "text", notNull: true},
	{table: TableListeners, name: "spec", sqlType: "jsonb", notNull: true},
	{table: TableListeners, name: "spec_hash", sqlType: "text", notNull: true},
	{table: TableListeners, name: "target_table", sqlType: "text", notNull: true},
	{table: TableListeners, name: "target_oid", sqlType: "oid", notNull: true},
	{table: TableListeners, name: "enabled", sqlType: "boolean", notNull: true},
	{table: TableListeners, name: "applied_at", sqlType: "timestamptz", notNull: true},

	{table: TableListenerTriggers, name: "listener", sqlType: "text", notNull: true},
	{table: TableListenerTriggers, name: "operation", sqlType: "text", notNull: true},
	{table: TableListenerTriggers, name: "trigger_name", sqlType: "text", notNull: true},
	{table: TableListenerTriggers, name: "function_name", sqlType: "text", notNull: true},
	{table: TableListenerTriggers, name: "ddl_hash", sqlType: "text", notNull: true},

	{table: TableEvents, name: "id", sqlType: "bigint", notNull: true},
	{table: TableEvents, name: "listener", sqlType: "text", notNull: true},
	{table: TableEvents, name: "table_name", sqlType: "text", notNull: true},
	{table: TableEvents, name: "operation", sqlType: "text", notNull: true},
	{table: TableEvents, name: "payload", sqlType: "jsonb", notNull: true},
	{table: TableEvents, name: "txid", sqlType: "xid8", notNull: true},
	{table: TableEvents, name: "occurred_at", sqlType: "timestamptz", notNull: true},

	{table: TableEventQueue, name: "event_id", sqlType: "bigint", notNull: true},
	// occurred_at is here under AC 39's ratified answer and under no other reading of it. The
	// column without the key composes nothing, so migrationddl_test.go asserts the key separately.
	{table: TableEventQueue, name: "occurred_at", sqlType: "timestamptz", notNull: true},
	{table: TableEventQueue, name: "listener", sqlType: "text", notNull: true},
	{table: TableEventQueue, name: "status", sqlType: "text", notNull: true},
	{table: TableEventQueue, name: "attempts", sqlType: "int", notNull: true},
	{table: TableEventQueue, name: "next_attempt_at", sqlType: "timestamptz", notNull: true},
	{table: TableEventQueue, name: "leased_until", sqlType: "timestamptz", notNull: false},
	{table: TableEventQueue, name: "leased_by", sqlType: "text", notNull: false},
	{table: TableEventQueue, name: "dead_reason", sqlType: "text", notNull: false},

	{table: TableDeliveries, name: "event_id", sqlType: "bigint", notNull: true},
	{table: TableDeliveries, name: "attempt", sqlType: "int", notNull: true},
	// http_status is nullable deliberately: a transport error, a timeout or a DNS failure produces
	// no status at all and internal/delivery must still record that the attempt happened.
	{table: TableDeliveries, name: "http_status", sqlType: "int", notNull: false},
	{table: TableDeliveries, name: "response_snippet", sqlType: "text", notNull: false},
	{table: TableDeliveries, name: "error", sqlType: "text", notNull: false},
	{table: TableDeliveries, name: "duration_ms", sqlType: "int", notNull: true},
	{table: TableDeliveries, name: "created_at", sqlType: "timestamptz", notNull: true},
}

func TestTheObjectContractSizeIsPinned(t *testing.T) {
	if len(theObjectContract) != 38 {
		t.Fatalf("the contract lists %d columns across the six tables; update this count with the "+
			"set, or a column dropped from it stops being asserted", len(theObjectContract))
	}
}

// migrationCreating is the version that creates one object, read from doc.go's set rather than
// written out again here (Architecture "Reuses From": the DDL and the exported contract drift the
// moment either re-lists what the other declares).
func migrationCreating(t *testing.T, name string) int {
	t.Helper()

	for _, object := range Objects {
		if object.Name == name {
			return object.Migration
		}
	}
	t.Fatalf("doc.go declares no object named %q", name)
	return 0
}

// declaredColumnsByTable is every column both migrations declare, keyed by table, with a failure
// for any line inside a CREATE TABLE block the reader could not read -- so a column written in an
// unexpected shape is reported rather than silently absent from both directions below.
func declaredColumnsByTable(t *testing.T) map[string][]sqlColumn {
	t.Helper()

	byTable := map[string][]sqlColumn{}
	for _, found := range embeddedCorpusOrFail(t) {
		columns, unread := declaredColumnsIn(found.sql)
		for _, line := range unread {
			t.Errorf("%s holds a line inside a table block that declares neither a column nor a "+
				"constraint: %q", found.file, line)
		}
		for _, column := range columns {
			byTable[column.table] = append(byTable[column.table], column)
		}
	}
	return byTable
}

// TestEveryContractColumnIsDeclaredWithItsTypeAndNullability walks the contract, not the DDL, so a
// column the DDL never declares fails by name.
func TestEveryContractColumnIsDeclaredWithItsTypeAndNullability(t *testing.T) {
	declared := declaredColumnsByTable(t)
	for _, want := range theObjectContract {
		version := migrationCreating(t, want.table)
		found := slices.IndexFunc(declared[want.table], func(c sqlColumn) bool {
			return c.name == want.name
		})
		if found < 0 {
			t.Errorf("migration %d declares no %s.%s, which the packages above this one compile against",
				version, want.table, want.name)
			continue
		}
		assertColumnMatchesContract(t, declared[want.table][found], want)
	}
}

func assertColumnMatchesContract(t *testing.T, got sqlColumn, want contractColumn) {
	t.Helper()

	if got.sqlType != want.sqlType {
		t.Errorf("%s.%s is declared %s, and the contract says %s",
			want.table, want.name, got.sqlType, want.sqlType)
	}
	if got.notNull != want.notNull {
		t.Errorf("%s.%s is declared notNull=%t, and the contract says %t",
			want.table, want.name, got.notNull, want.notNull)
	}
}

// TestTheEventIdentityIsTheOnlyGeneratedColumn pins the clause the contract writes into the id
// column and nothing else asserts. Global uniqueness of an event id comes solely from this single
// identity sequence -- measured, the catalog enforces none of it, since a row carrying a duplicate
// id in another partition is accepted -- so the sequence being there, and being the only one, is
// the whole of the property internal/reconcile and internal/delivery rely on.
func TestTheEventIdentityIsTheOnlyGeneratedColumn(t *testing.T) {
	generated := map[string]string{}
	for table, columns := range declaredColumnsByTable(t) {
		for _, column := range columns {
			if strings.Contains(column.rest, "GENERATED") {
				generated[table+"."+column.name] = strings.TrimSpace(column.rest)
			}
		}
	}

	want := map[string]string{TableEvents + ".id": "GENERATED ALWAYS AS IDENTITY"}
	if !maps.Equal(generated, want) {
		t.Errorf("the corpus generates %v, want exactly %v", generated, want)
	}
}

// TestEveryDeclaredColumnIsInTheContract is the other direction, and it hides a different mistake:
// a column this package creates that no later package was told about, which reads as harmless until
// internal/delivery has to guess whether it may write to it.
func TestEveryDeclaredColumnIsInTheContract(t *testing.T) {
	for table, columns := range declaredColumnsByTable(t) {
		for _, got := range columns {
			inContract := slices.ContainsFunc(theObjectContract, func(want contractColumn) bool {
				return want.table == table && want.name == got.name
			})
			if !inContract {
				t.Errorf("the DDL declares %s.%s and the object contract does not list it",
					table, got.name)
			}
		}
	}
}

//go:build integration

package schema

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is ADR-3's two named states against a real catalog, and the second of this step's
// two-violation inputs.
//
// The claim/refuse pair is one policy with two answers and they are asserted apart: an unmarked
// schema is the supported minimal-privilege path a DBA pre-creating it takes, and refusing it would
// break the path this package documents, while a schema another instance marked is a two-service
// corruption. Conflating them breaks exactly one of the two, and only one row would notice.

// theAheadSchema is a name that is reserved to *this* package and creatable by the server. M8: the
// server compares the first three bytes against lower-case `pg_`, so `PG_ahead` is a schema that can
// genuinely exist and genuinely be ahead of the binary -- which is what makes the input below
// violate steps 1 and 7 at once rather than step 1 alone.
const theAheadSchema = "PG_ahead"

// markerReadingOn is what the catalog says about one schema's ownership marker, read through the
// package's own reader so a fixture cannot disagree with the boot about what it found.
func markerReadingOn(t *testing.T, pool *pgxpool.Pool, schemaName, instance string) markerReading {
	t.Helper()

	marked, fault := schemaObject(schemaName, instance)
	if fault != IdentifierOK {
		t.Fatalf("name the schema %s for instance %s: it %s", schemaName, instance, fault)
	}
	reading, err := markerOn(t.Context(), pool, marked)
	if err != nil {
		t.Fatalf("read the ownership marker on %s: %v", schemaName, err)
	}
	return reading
}

// TestASchemaCarryingNoMarkerIsClaimedRatherThanRefused is ADR-3's claimable half.
func TestASchemaCarryingNoMarkerIsClaimedRatherThanRefused(t *testing.T) {
	skipIfShort(t)

	pool := emptySchemas(t, harnessSchema)
	cfg := aBootConfiguration(t)

	if state := markerReadingOn(t, pool, harnessSchema, cfg.Instance).state; state != markerAbsent {
		t.Fatalf("the pre-created schema reads as %q rather than %q, so this case would prove "+
			"nothing about an unmarked one", state, markerAbsent)
	}

	mustBootstrap(t, pool, cfg)

	reading := markerReadingOn(t, pool, harnessSchema, cfg.Instance)
	if reading.state != markerOurs || reading.named != cfg.Instance {
		t.Errorf("after the boot the schema reads as %q named %q, want %q named %q: an unmarked "+
			"schema is claimed, and a schema left unmarked is one every later drop guard refuses",
			reading.state, reading.named, markerOurs, cfg.Instance)
	}
}

// TestASchemaMarkedForAnotherInstanceIsRefused is ADR-3's refused half. The foreign marker is
// written through the package's own writer, so what the boot meets is the marker another instance
// would have left rather than a string this suite invented.
func TestASchemaMarkedForAnotherInstanceIsRefused(t *testing.T) {
	skipIfShort(t)

	pool := emptySchemas(t, harnessSchema)
	cfg := aBootConfiguration(t)
	claimFor(t, pool, harnessSchema, theForeignInstance)

	before := catalogInventory(t, pool, noSchemaExcluded)
	err := Bootstrap(t.Context(), pool, cfg)

	if !errors.Is(err, ErrForeignInstance) {
		t.Fatalf("a boot onto a schema marked for %s answered %v, want %v",
			theForeignInstance, err, ErrForeignInstance)
	}
	for _, named := range []string{theForeignInstance, cfg.Instance} {
		if !strings.Contains(err.Error(), named) {
			t.Errorf("the refusal %q does not name %q", err, named)
		}
	}
	assertInventoryUnchanged(t, before, catalogInventory(t, pool, noSchemaExcluded),
		"a boot refused for a foreign ownership marker")
}

// claimFor writes one instance's ownership marker onto one schema.
func claimFor(t *testing.T, pool *pgxpool.Pool, schemaName, instance string) {
	t.Helper()

	marked, fault := schemaObject(schemaName, instance)
	if fault != IdentifierOK {
		t.Fatalf("name the schema %s for instance %s: it %s", schemaName, instance, fault)
	}
	if err := claimMarker(t.Context(), pool, marked); err != nil {
		t.Fatalf("mark %s as owned by %s: %v", schemaName, instance, err)
	}
}

// TestAReservedNameOnADatabaseAheadOfTheBinaryIsRefusedForTheName is the second of this step's
// two-violation inputs, and it pins step 1 before step 7. The first is container-free and pins step
// 1 before step 2.
//
// The second violation is established on its own first, against a schema whose name step 1 accepts
// but whose ledger records a version this binary does not embed. Without it the input below would
// violate step 1 alone, both orders would answer identically, and reversing them would change which
// reason an operator is shown with the suite still green.
func TestAReservedNameOnADatabaseAheadOfTheBinaryIsRefusedForTheName(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	cfg := aBootConfiguration(t)

	const usableName = "ahead"
	recorded := plantALedgerAheadOfTheBinary(t, pool, usableName)
	cfg.Database.Schema = usableName
	if atStepSeven := Bootstrap(t.Context(), pool, cfg); !errors.Is(atStepSeven, ErrSchemaAhead) {
		t.Fatalf("a database recording version %d answered %v, want %v; without a second violation "+
			"that really fires, the row below pins no order", recorded, atStepSeven, ErrSchemaAhead)
	}

	plantALedgerAheadOfTheBinary(t, pool, theAheadSchema)
	cfg.Database.Schema = theAheadSchema
	both := Bootstrap(t.Context(), pool, cfg)

	if both == nil {
		t.Fatal("an input violating steps 1 and 7 both answered no error")
	}
	if want := reservedSchemaName(theAheadSchema).Error(); both.Error() != want {
		t.Errorf("an input violating steps 1 and 7 was refused as %q, want the step 1 answer %q",
			both, want)
	}
	if errors.Is(both, ErrSchemaAhead) {
		t.Errorf("the boot reported the recorded version rather than the schema name, so step 7 " +
			"runs before step 1 and an operator is sent to fix the wrong thing")
	}
}

// plantALedgerAheadOfTheBinary creates one schema holding a ledger that records a version this
// binary does not embed, and answers with that version. The version is derived from the shipped
// corpus rather than written down, so a corpus that grows cannot leave this fixture behind.
func plantALedgerAheadOfTheBinary(t *testing.T, pool *pgxpool.Pool, schemaName string) int {
	t.Helper()

	mustExecOn(t, pool, "CREATE SCHEMA "+mustQuote(t, schemaName))
	ledger := mustQualify(t, schemaName, TableSchemaVersion)
	mustExecOn(t, pool, "CREATE TABLE "+ledger+" (version integer PRIMARY KEY, checksum text NOT "+
		"NULL, applied_at timestamptz NOT NULL DEFAULT now())")

	recorded := highestEmbedded(embeddedCorpusOrFail(t)) + 1
	mustExecOn(t, pool, "INSERT INTO "+ledger+" (version, checksum) VALUES ($1, $2)",
		recorded, "a checksum no file of this binary hashes to")
	return recorded
}

package schema

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 2, and it is container-free for the reason the criterion is about: the
// refusal fires before any connection is used, so a row needing a database would already have
// disproved the property it was written for.
//
// The mixed-case rows are the load-bearing ones. M8 measured the server *accepting* a quoted
// `CREATE SCHEMA` for a name whose first three bytes are not exactly lower-case `pg_`, and this
// package quotes every configured name (skill Pattern 7) -- so this client-side check is the only
// thing closing the class rather than a second line of defence.

// The two production sources this step ships, named once so that the scans in bootstrapscan_test.go
// and the reconciliations below read them rather than repeating the strings.
const (
	theBootstrap       = "bootstrap.go"
	theBootstrapSchema = "bootstrapschema.go"
)

// theReservedPrefix is the prefix R4 and PostgreSQL both name, written here as the literal rather
// than read from identifier.go: an expectation derived from the code under test is satisfied by
// whatever that code happens to say.
const theReservedPrefix = "pg_"

// theUnreachableDSN names a port nothing listens on, so a pool built from it parses, opens lazily,
// and refuses the first connection anyone asks it for. It is what makes a row that reached step 2
// distinguishable from one refused at step 1.
const theUnreachableDSN = "postgres://noty:noty@127.0.0.1:1/noty?sslmode=disable"

// theUnreachableDeadline bounds a connection attempt this suite expects to fail, so a row that
// somehow reached a server fails rather than hanging the package.
const theUnreachableDeadline = 10 * time.Second

// configWithSchema is the smallest configuration Bootstrap reads: the schema it is pointed at, the
// instance the lock key and the ownership marker are scoped by, and the retention its horizon guard
// counts partitions from.
//
// The durations are internal/config's own built-in defaults, which its doc.go gives as
// partition_interval 24h and precreate and keep 168h. They are here rather than left zero because a
// zero partition_interval is a value R10 refuses, and the arithmetic that reads one divides by it
// -- so a fixture carrying zeroes would describe a configuration internal/config cannot produce
// and would panic where a boot from a real configuration answers. What that input does, and that
// the horizon guard moved *where* it panics rather than introducing the panic, is pinned by
// TestAnIntervalR10RefusesPanicsInTheGuardJustAsItAlreadyDidInTheArithmetic.
func configWithSchema(schema string) config.Config {
	return config.Config{
		Instance:  schema,
		Database:  config.Database{Schema: schema, URL: theUnreachableDSN},
		Retention: retentionOf(24*time.Hour, 168*time.Hour, 168*time.Hour),
	}
}

// unreachablePool is a pool onto nowhere. It is built directly rather than through OpenPool because
// OpenPool connects before returning (ADR-2) and this suite needs a pool that exists and cannot
// serve, which is exactly the state step 2 fails in.
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool, err := pgxpool.New(context.Background(), theUnreachableDSN)
	if err != nil {
		t.Fatalf("build a pool onto an unreachable address: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// bounded is a context a connection attempt cannot outlive.
func bounded(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), theUnreachableDeadline)
	t.Cleanup(cancel)
	return ctx
}

// TestAReservedSchemaNameIsRefusedInAnyLetterCase is criterion 2.
//
// Every accepted row differs from a refused one in the guarded property alone, so a check widened
// past the prefix fails a named row instead of quietly refusing valid configurations. The refusal
// is asserted by equality against the message the refusal builds, and its two clauses -- the
// schema and the prefix -- are asserted separately, so emptying either fails the clause named for
// it.
func TestAReservedSchemaNameIsRefusedInAnyLetterCase(t *testing.T) {
	for _, tc := range []struct {
		name, schema string
		refused      bool
	}{
		{name: "all lower case", schema: "pg_noty", refused: true},
		{name: "mixed case", schema: "PG_Noty", refused: true},
		{name: "upper case", schema: "PG_NOTY", refused: true},
		{name: "one letter of the prefix upper case", schema: "pG_noty", refused: true},
		{name: "exactly the prefix, as long as the guard's bound", schema: "pg_", refused: true},
		{name: "exactly the prefix, mixed case", schema: "Pg_", refused: true},

		{name: "the prefix one byte short of its bound", schema: "pg", refused: false},
		{name: "the letters without the separator", schema: "pgnoty", refused: false},
		{name: "the separator without the letters", schema: "_pg_noty", refused: false},
		{name: "the default schema", schema: "noty", refused: false},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				err := Bootstrap(bounded(t), unreachablePool(t), configWithSchema(tc.schema))
				if err == nil {
					t.Fatalf(
						"Bootstrap onto an unreachable pool answered no error for schema %q",
						tc.schema,
					)
				}

				refused := err.Error() == reservedSchemaName(tc.schema).Error()
				if refused != tc.refused {
					t.Fatalf(
						"schema %q was refused as reserved = %t, want %t; the answer was %q",
						tc.schema, refused, tc.refused, err,
					)
				}
				if tc.refused {
					assertRefusalNamesTheSchemaAndThePrefix(t, err, tc.schema)
				}
			},
		)
	}
}

// assertRefusalNamesTheSchemaAndThePrefix checks the two things criterion 2 requires the message to
// carry, each on its own, so an operator can act on it.
func assertRefusalNamesTheSchemaAndThePrefix(t *testing.T, err error, schema string) {
	t.Helper()

	if !strings.Contains(err.Error(), schema) {
		t.Errorf("the refusal %q does not name the schema %q it refused", err, schema)
	}
	if !strings.Contains(err.Error(), theReservedPrefix) {
		t.Errorf(
			"the refusal %q does not name the reserved prefix %q, so an operator is told a "+
				"name is wrong and not which rule it broke", err, theReservedPrefix,
		)
	}
}

// TestTheReservedNameRefusalUsesNoConnectionAtAll is criterion 2's "before any statement", asserted
// as strongly as it can be asserted: the pool handed in is nil, so any implementation reaching step
// 2 before step 1 dereferences it rather than answering. A refusal that needed a database could not
// have fired before one was used.
func TestTheReservedNameRefusalUsesNoConnectionAtAll(t *testing.T) {
	const schema = "PG_Noty"

	err := Bootstrap(t.Context(), nil, configWithSchema(schema))

	if err == nil || err.Error() != reservedSchemaName(schema).Error() {
		t.Fatalf("Bootstrap with no pool at all answered %v, want the reserved-name refusal", err)
	}
}

// TestAReservedNameIsRefusedBeforeAConnectionThatCannotBeMade is the first of this step's two
// two-violation inputs. Its second half lives in bootstrapownership_integration_test.go and
// pins step 1 before step 7.
//
// The second violation is established first, against the same pool: without it the row would be an
// input violating step 1 alone, and both orders would answer identically.
func TestAReservedNameIsRefusedBeforeAConnectionThatCannotBeMade(t *testing.T) {
	pool := unreachablePool(t)

	usable := configWithSchema("noty")
	atStepTwo := Bootstrap(bounded(t), pool, usable)
	if atStepTwo == nil {
		t.Fatal(
			"a pool onto an unreachable address served a connection, so the input below " +
				"violates step 1 alone and pins no order",
		)
	}

	const reserved = "pg_noty"
	both := Bootstrap(bounded(t), pool, configWithSchema(reserved))
	if both == nil {
		t.Fatal("an input violating steps 1 and 2 both answered no error")
	}
	if want := reservedSchemaName(reserved).Error(); both.Error() != want {
		t.Errorf(
			"an input violating steps 1 and 2 was refused as %q, want the step 1 answer %q; "+
				"the documented order reports the name and not the connection", both, want,
		)
	}
	if both.Error() == atStepTwo.Error() {
		t.Errorf("both violations answered %q, which is what step 2 alone answers", both)
	}
}

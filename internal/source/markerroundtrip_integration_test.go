//go:build integration

package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The marker's round-trip half: what internal/reconcile reads back out of pg_description to decide
// what it owns. The emitted text is not evidence here -- a COMMENT that parsed and attached to nothing looks
// identical to one that took effect -- so every claim below is read from the catalog through
// objectcomments_integration_test.go's two readers.

const theMarkerTarget = "marker_target"

// installMarkerObjects applies one request's object sets against a fresh target and returns them.
func installMarkerObjects(
	t *testing.T, pool *pgxpool.Pool, instance, listener string,
	operations ...config.Operation,
) []ObjectSet {
	t.Helper()
	mustExecOn(t, pool, `CREATE TABLE public.`+theMarkerTarget+` (id int, status text)`)
	request := generationRequest(operations...)
	request.Instance, request.Listener.Name, request.Target.Table = instance, listener, theMarkerTarget
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, set := range sets {
		executeObjectSet(t, pool, set)
	}
	return sets
}

// TestBothStoredCommentsAreTheEmittedMarkerWithTheLongOperationSpelling reads both comments for all
// three operations. The two statements quote the same marker independently, so a mistake in one is
// invisible in the other, and the long/abbreviated pair is what the ownership parse turns on:
// the object *name* carries `ins`/`upd`/`del` and the comment must carry `insert`/`update`/`delete`.
func TestBothStoredCommentsAreTheEmittedMarkerWithTheLongOperationSpelling(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	sets := installMarkerObjects(
		t, pool, "prod", "order_paid", config.Operation{Kind: "insert"},
		config.Operation{Kind: "update"}, config.Operation{Kind: "delete"},
	)
	if len(sets) != 3 {
		t.Fatalf("three operations produced %d object sets, so this case covers fewer than it names", len(sets))
	}

	for _, set := range sets {
		t.Run(
			set.Operation, func(t *testing.T) {
				for name, stored := range storedCommentsOf(t, pool, set) {
					if stored != set.Marker {
						t.Errorf(
							"the %s comment is %q, want the emitted marker %q byte for byte",
							name, stored, set.Marker,
						)
					}
					if !strings.HasSuffix(stored, ":"+set.Operation) {
						t.Errorf(
							"the %s comment %q does not end in the long spelling %q",
							name, stored, set.Operation,
						)
					}
					abbreviation, err := operationAbbreviationFor(set.Operation)
					if err != nil {
						t.Fatal(err)
					}
					if strings.HasSuffix(stored, ":"+abbreviation) {
						t.Errorf(
							"the %s comment %q ends in the object name's abbreviation, which internal/reconcile "+
								"cannot attribute to an operation", name, stored,
						)
					}
				}
			},
		)
	}
}

// storedCommentsOf reads both of one object set's comments, keyed by which object carries it, so
// each assertion above runs against the function's and the trigger's alike.
func storedCommentsOf(t *testing.T, pool *pgxpool.Pool, set ObjectSet) map[string]string {
	t.Helper()
	return map[string]string{
		"function": functionComment(t, pool, harnessSchema, set.FunctionName),
		"trigger":  triggerComment(t, pool, "public", theMarkerTarget, set.TriggerName),
	}
}

// TestTheHundredAndOneByteMarkerRoundTripsWhole is the row that fails for exactly one
// implementation: the one applying the 63-byte identifier bound to a comment body, which would
// truncate the listener and operation fields internal/reconcile uses for ownership and leave every
// shorter marker passing. The lengths come from marker_test.go's derivation of the ceiling --
// internal/config's 41-byte maximum for each of the two name fields -- rather than from a second
// count of them.
func TestTheHundredAndOneByteMarkerRoundTripsWhole(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	instance, listener := strings.Repeat("i", 41), strings.Repeat("l", 41)
	sets := installMarkerObjects(
		t, pool, instance, listener,
		config.Operation{Kind: theLongestOperationSpelling},
	)
	if len(sets) != 1 || len(sets[0].Marker) != 101 {
		t.Fatalf(
			"the ceiling request rendered %d sets whose marker is %d bytes, want one of 101; "+
				"this case cannot discriminate a truncating implementation otherwise",
			len(sets), len(sets[0].Marker),
		)
	}

	for name, stored := range storedCommentsOf(t, pool, sets[0]) {
		if stored != sets[0].Marker {
			t.Errorf(
				"the %s comment round-tripped %d bytes as %q, want all %d of %q -- nothing "+
					"truncated, elided or normalised", name, len(stored), stored,
				len(sets[0].Marker), sets[0].Marker,
			)
		}
	}
}

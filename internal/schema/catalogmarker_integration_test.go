//go:build integration

package schema

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file puts the marker's four states in front of the running server, one real catalog state
// each, rather than constructing them in Go: the discrimination has to hold against what PostgreSQL
// actually stores, since Step 14's first drop guard reads the table form inside its own transaction.

// foreignInstanceName is the other service sharing this database in the foreign-marker rows. It is
// not a prefix or a suffix of ourInstance, so a comparison that only tested containment would still
// have to answer foreign.
const foreignInstanceName = "reporting"

// markedSubjects are the two forms, as objects of the fixture's own schema.
var markedSubjects = []struct {
	name  string
	build func(instance string) (markedObject, IdentifierFault)
}{
	{name: "the service schema", build: func(instance string) (markedObject, IdentifierFault) {
		return schemaObject(harnessSchema, instance)
	}},
	{name: "a partition", build: func(instance string) (markedObject, IdentifierFault) {
		return partitionObject(harnessSchema, PartitionDefault, instance)
	}},
}

// mustMark builds one marked object, failing rather than returning a fault a test would ignore.
func mustMark(t *testing.T, build func(string) (markedObject, IdentifierFault), instance string) markedObject {
	t.Helper()

	object, fault := build(instance)
	if fault != IdentifierOK {
		t.Fatalf("build the marked object for instance %s: its name %s", instance, fault)
	}
	return object
}

// commentByHand writes an arbitrary comment on one object, which is how the three states this
// package does not write are planted.
func commentByHand(t *testing.T, pool *pgxpool.Pool, object markedObject, text string) {
	t.Helper()

	var statement string
	err := pool.QueryRow(t.Context(), renderStatementQuery, object.commentOn, object.target, text).
		Scan(&statement)
	if err != nil {
		t.Fatalf("render a comment on %s: %v", object.target, err)
	}
	mustExecOn(t, pool, statement)
}

// commentOnRecord is the comment the catalog holds for one object, read independently of the state
// the reader would put it in.
func commentOnRecord(t *testing.T, pool *pgxpool.Pool, object markedObject) (string, bool) {
	t.Helper()

	var written *string
	if err := pool.QueryRow(t.Context(), object.readQuery, object.target).Scan(&written); err != nil {
		t.Fatalf("read the comment on %s: %v", object.target, err)
	}
	if written == nil {
		return "", false
	}
	return *written, true
}

// TestTheOwnershipMarkerRoundTripsVerbatimInBothForms asserts what claimMarker wrote against the
// Ownership-marker contract's own spelling rather than against the constants that built it: a
// comparison with MarkerPrefix would agree with any prefix, including one a later edit changed.
func TestTheOwnershipMarkerRoundTripsVerbatimInBothForms(t *testing.T) {
	skipIfShort(t)

	pool := eventLogFixture(t)
	for _, subject := range markedSubjects {
		t.Run(subject.name, func(t *testing.T) {
			object := mustMark(t, subject.build, ourInstance)
			if err := claimMarker(t.Context(), pool, object); err != nil {
				t.Fatalf("claim %s: %v", object.target, err)
			}

			written, present := commentOnRecord(t, pool, object)
			want := map[string]string{
				"the service schema": "pg_noty:v1:noty",
				"a partition":        "pg_noty:v1:noty:partition",
			}[subject.name]
			if !present || written != want {
				t.Errorf("%s carries %q (present=%t), want %q", object.target, written, present, want)
			}
		})
	}
}

// TestEveryMarkerStateIsReachedAsARealCatalogStateAndAnsweredAsItself is SC-4 against the server.
// None of the four is a fallthrough: ADR-3 claims an absent marker because a DBA pre-creating the
// schema is the supported minimal-privilege path, and refuses a foreign one with ErrForeignInstance
// because claiming another instance's schema corrupts two services at once. An unreadable marker is
// its own answer again, since defaulting it to either of those is a fail-open.
func TestEveryMarkerStateIsReachedAsARealCatalogStateAndAnsweredAsItself(t *testing.T) {
	skipIfShort(t)

	for _, subject := range markedSubjects {
		for _, state := range []struct {
			name string
			// plant writes the catalog state, or writes nothing for the absent one.
			plant func(t *testing.T, pool *pgxpool.Pool, object markedObject)
			want  markerState
			// wantNamed is the instance the reading must carry, which only a readable marker has.
			wantNamed string
		}{
			{name: "no comment at all", want: markerAbsent,
				plant: func(*testing.T, *pgxpool.Pool, markedObject) {}},
			{name: "this instance's own marker", want: markerOurs, wantNamed: ourInstance,
				plant: func(t *testing.T, pool *pgxpool.Pool, object markedObject) {
					if err := claimMarker(t.Context(), pool, object); err != nil {
						t.Fatalf("claim %s: %v", object.target, err)
					}
				}},
			{name: "a note somebody wrote by hand", want: markerUnreadable,
				plant: func(t *testing.T, pool *pgxpool.Pool, object markedObject) {
					commentByHand(t, pool, object, "created for the 2026 migration, do not drop")
				}},
			{name: "a marker of a later format version", want: markerUnreadable,
				plant: func(t *testing.T, pool *pgxpool.Pool, object markedObject) {
					commentByHand(t, pool, object, "pg_noty:v2:"+ourInstance)
				}},
			{name: "another instance's marker", want: markerForeign, wantNamed: foreignInstanceName,
				plant: func(t *testing.T, pool *pgxpool.Pool, object markedObject) {
					commentByHand(t, pool, object,
						mustMark(t, subject.build, foreignInstanceName).form.text(foreignInstanceName))
				}},
		} {
			t.Run(subject.name+", "+state.name, func(t *testing.T) {
				pool := eventLogFixture(t)
				object := mustMark(t, subject.build, ourInstance)
				state.plant(t, pool, object)

				got, err := markerOn(t.Context(), pool, object)
				if err != nil {
					t.Fatalf("read the marker on %s: %v", object.target, err)
				}
				if got.state != state.want {
					written, present := commentOnRecord(t, pool, object)
					t.Errorf("%s carrying %q (present=%t) reads as %q, want %q",
						object.target, written, present, got.state, state.want)
				}
				// ADR-3 refuses a foreign marker with ErrForeignInstance, and errors.go's
				// foreignInstance names both instances -- so the reading has to carry the name the
				// catalog held, or an operator cannot tell a misconfigured instance from two
				// services sharing one schema.
				if got.named != state.wantNamed {
					t.Errorf("%s reads as %q naming instance %q, want it to name %q",
						object.target, got.state, got.named, state.wantNamed)
				}
			})
		}
	}
}

// TestAnInstanceNameCarryingAQuoteIsWrittenAsOneLiteral is why the statement is assembled by the
// server's own format(%L) rather than here. An instance holding a quote would close the literal
// early under any hand-rolled quoting, and the marker is the mechanism every destructive guard
// reads -- a marker that failed to write is a partition retention will refuse to drop forever.
func TestAnInstanceNameCarryingAQuoteIsWrittenAsOneLiteral(t *testing.T) {
	skipIfShort(t)

	const awkward = "it's mine; DROP SCHEMA noty --"

	pool := eventLogFixture(t)
	object := mustMark(t, markedSubjects[0].build, awkward)
	if err := claimMarker(t.Context(), pool, object); err != nil {
		t.Fatalf("claim the schema for instance %q: %v", awkward, err)
	}

	if written, present := commentOnRecord(t, pool, object); !present || written != "pg_noty:v1:"+awkward {
		t.Fatalf("the schema carries %q (present=%t), want %q", written, present, "pg_noty:v1:"+awkward)
	}
	if got, err := markerOn(t.Context(), pool, object); err != nil || got.state != markerOurs ||
		got.named != awkward {
		t.Errorf("the marker reads as (%q, %q, %v), want (%q, %q, nil)",
			got.state, got.named, err, markerOurs, awkward)
	}
}

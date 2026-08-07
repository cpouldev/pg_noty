//go:build integration

package schema

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is the one measurement plan.go's horizon bound rests on, taken against the running server
// rather than transcribed from a document.
//
// The bound is an argument in two steps, and each step is a clause below, because either one failing
// on its own would leave the bound wrong in a different way:
//
//  1. Every relation carries a pg_class.oid, so the number of relations a database can hold is the
//     number of distinct oids. A pg_class whose oid column stopped being of type oid breaks the
//     argument at its first step.
//  2. An oid is an unsigned four-byte integer, so there are exactly 2^32 of them. A release that
//     widened the type would leave plan.go refusing configurations the server had grown able to
//     serve -- which is the safe direction, and still a bound that had stopped being derived.
//
// TestTheHorizonBoundIsTheRelationIdentifierSpace is the container-free twin, pinning the constant
// against the literal these two clauses produce.

const (
	// theOidColumnOfPgClassQuery reads the type of the column every relation is identified by. It
	// asks the catalog what the column *is* rather than assuming it, because clause 1 is the half of
	// the argument that a schema change would break silently.
	theOidColumnOfPgClassQuery = `SELECT atttypid::regtype::text
FROM pg_catalog.pg_attribute
WHERE attrelid = 'pg_catalog.pg_class'::regclass AND attname = 'oid'`
	// theOidCastQuery casts one decimal literal to an oid, and answers whether the server accepted
	// it. It is a cast rather than a width read off pg_type, because what the bound needs is the set
	// of values the type admits and a byte width is one inference away from that.
	theOidCastQuery = `SELECT to_regtype('oid') IS NOT NULL AND $1::text::oid IS NOT NULL`
)

// TestTheServerNamesRelationsInAThirtyTwoBitIdentifierSpace is both clauses, each asserted on its
// own so a failure says which step of the argument gave way.
//
// The pair of literals is the equality case of the type's own range: 4294967295 is the largest value
// an unsigned four-byte integer expresses and 4294967296 is the first it does not, so only a type of
// exactly that width accepts the one and refuses the other.
func TestTheServerNamesRelationsInAThirtyTwoBitIdentifierSpace(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)

	var identifiedBy string
	if err := pool.QueryRow(t.Context(), theOidColumnOfPgClassQuery).Scan(&identifiedBy); err != nil {
		t.Fatalf("read the type of the column every relation is identified by: %v", err)
	}
	if identifiedBy != "oid" {
		t.Errorf("a relation is identified by a %s and not an oid, so the count of relations a "+
			"database can hold is no longer the count of oids and plan.go's bound is derived from "+
			"the wrong type", identifiedBy)
	}

	const largest, firstBeyond = "4294967295", "4294967296"
	if !oidAccepts(t, pool, largest) {
		t.Errorf("the server refuses %s as an oid, so the identifier space is narrower than the "+
			"%d plan.go bounds a plan by", largest, relationIdentifiersOneDatabaseHolds)
	}
	if oidAccepts(t, pool, firstBeyond) {
		t.Errorf("the server accepts %s as an oid, so the space is wider than the %d plan.go bounds "+
			"a plan by and the bound has stopped being the type's own",
			firstBeyond, relationIdentifiersOneDatabaseHolds)
	}
}

// oidAccepts reports whether the server reads one decimal literal as an oid.
//
// Only a refusal the *server* raised answers false. A dropped socket, a cancelled context or a pool
// with nothing left to serve carry no condition at all, and reading one of those as "the server
// refused this value" would make a broken connection look exactly like the narrow identifier space
// this measurement exists to detect. The condition is read through the package's own reader rather
// than a second one.
func oidAccepts(t *testing.T, pool *pgxpool.Pool, written string) bool {
	t.Helper()

	var accepted bool
	err := pool.QueryRow(t.Context(), theOidCastQuery, written).Scan(&accepted)
	if err == nil {
		return accepted
	}
	if _, fromServer := serverRefusalIn(err); !fromServer {
		t.Fatalf("casting %s to an oid failed without the server saying anything: %v", written, err)
	}
	return false
}

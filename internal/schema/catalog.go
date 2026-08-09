package schema

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This file is the package's single observation authority: the only source that reads the system
// catalogs, asserted by TestCatalogIsTheOnlyProductionSourceThatNamesTheSystemCatalogs. Steps 11,
// 13, 14 and 15 all ask it what is true, so maintain.go and retention.go cannot drift apart in how
// they see the world -- and drift there is a create and a drop disagreeing about what exists.
//
// Every query is confined to the configured schema. Bounds are parsed back by partitionbound.go and
// markers by ownershipmarker.go, both container-free (M12); identifiers are rendered by
// identifier.go and never by a bind parameter, which cannot name an object at all (Pattern 7).

// catalogReader is the part of a pgx connection this file observes through. A pool, a pinned
// connection and a transaction all satisfy it, which is what lets Step 14 read the marker and the
// attachment inside the very transaction that performs the drop (ADR-6).
type catalogReader interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// catalogWriter adds the statement the ownership claim issues.
type catalogWriter interface {
	catalogReader
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// The queries. They live here rather than beside their callers because this file's whole claim is
// that no other source reads a system catalog; ownershipmarker.go reads two of them by name.
//
// The bound is read as two columns, and the pair is the whole of how a dropped partition is told
// from an unreadable one. They answer from different snapshots: relpartbound is a column of the row
// the scan produced, so it comes from this transaction's snapshot, while pg_get_expr resolves the
// relation afresh and answers NULL once it is gone. A row that declares a bound and renders none is
// therefore a partition dropped between the two -- the expected case under the three-replica
// retention this package supports -- and cataloginventory.go skips exactly that pair. Coalescing
// the NULL away is what this query used to do, and it destroyed the distinction here, where it is
// the only place either fact exists.
const (
	observePartitionsQuery = `
SELECT child.relname,
       childns.nspname,
       child.relpartbound IS NOT NULL,
       pg_get_expr(child.relpartbound, child.oid)
FROM pg_catalog.pg_inherits AS attachment
JOIN pg_catalog.pg_class AS child ON child.oid = attachment.inhrelid
JOIN pg_catalog.pg_namespace AS childns ON childns.oid = child.relnamespace
JOIN pg_catalog.pg_class AS parent ON parent.oid = attachment.inhparent
JOIN pg_catalog.pg_namespace AS parentns ON parentns.oid = parent.relnamespace
WHERE parentns.nspname = $1 AND parent.relname = $2
ORDER BY child.relname`

	attachedPartitionQuery = `
SELECT EXISTS (
  SELECT 1
  FROM pg_catalog.pg_inherits AS attachment
  JOIN pg_catalog.pg_class AS child ON child.oid = attachment.inhrelid
  JOIN pg_catalog.pg_namespace AS childns ON childns.oid = child.relnamespace
  JOIN pg_catalog.pg_class AS parent ON parent.oid = attachment.inhparent
  JOIN pg_catalog.pg_namespace AS parentns ON parentns.oid = parent.relnamespace
  WHERE childns.nspname = $1 AND child.relname = $2
    AND parentns.nspname = $1 AND parent.relname = $3)`

	// The two marker reads locate their object through to_reg*, which answers NULL for one that
	// does not exist rather than raising, and which resolves the quoted qualified name
	// identifier.go rendered. A schema nobody has created and one nobody has commented are both
	// unmarked, and ADR-3 claims both.
	schemaMarkerQuery    = `SELECT obj_description(to_regnamespace($1), 'pg_namespace')`
	partitionMarkerQuery = `SELECT obj_description(to_regclass($1), 'pg_class')`

	// COMMENT ON takes a string constant and not a bind parameter, so the marker has to be quoted
	// as a literal. format's %L is the server's own literal quoter, which is why the statement is
	// assembled there; %s carries the object name identifier.go has already rendered. The casts are
	// load-bearing: format's arguments are VARIADIC "any", and an uncast parameter there is one the
	// server refuses to infer a type for (SQLSTATE 42P18, measured).
	commentOnSchema      = `COMMENT ON SCHEMA %s IS %L`
	commentOnTable       = `COMMENT ON TABLE %s IS %L`
	renderStatementQuery = `SELECT format($1::text, $2::text, $3::text)`

	countRowsPrefix     = `SELECT count(*) FROM `
	transactionNowQuery = `SELECT now()`
)

// observePartitions is every partition attached to one parent in the configured schema, except one
// that ceased to exist while this very query was reading it. Everything else it cannot read fails
// the whole call rather than being left out, because a partition missing from the observation is one
// a drop decision was computed without; the one exception, and why omitting a partition that is
// already gone is not the same omission, is cataloginventory.go's subject.
func observePartitions(ctx context.Context, db catalogReader,
	schema, parent string) (observedPartitions, error) {
	rows, err := db.Query(ctx, observePartitionsQuery, schema, parent)
	if err != nil {
		return observedPartitions{}, finished(err)
	}
	defer rows.Close()

	var found observedPartitions
	for rows.Next() {
		var name, within string
		var declaresABound bool
		var rendered *string
		if err := rows.Scan(&name, &within, &declaresABound, &rendered); err != nil {
			return observedPartitions{}, finished(err)
		}
		if err := found.add(schema, name, within, boundReadFrom(declaresABound, rendered)); err != nil {
			return observedPartitions{}, err
		}
	}
	return found, finished(rows.Err())
}

// defaultPartitionRows is how many rows the DEFAULT partition holds (ADR-8's standing condition,
// which Step 11 stores into MaintenanceStats.DefaultPartitionRows on every pass).
//
// Counted against that partition alone and never through the parent: count(*) on the parent reads
// every partition, turning a per-pass observation into a full scan of the customer's largest table.
// It is exact rather than an estimate because the gauge is thresholded on "more than none", and
// pg_class.reltuples reads -1 for a partition nothing has analysed yet -- precisely the freshly
// blocked partition the gauge exists to notice.
//
// Zero is a reading, not a failure: a healthy DEFAULT partition holds no rows, and a caller must
// tell that from "could not observe". An unusable name is refused instead, so a parent carrying no
// DEFAULT partition cannot be read as one holding none.
func defaultPartitionRows(ctx context.Context, db catalogReader,
	schema, partition string) (int64, error) {
	target, fault := Qualified(schema, partition)
	if fault != IdentifierOK {
		return 0, finished(fmt.Errorf("the DEFAULT partition of schema %s cannot be counted: the "+
			"name %s given for it %s", schema, partition, fault))
	}

	var held int64
	if err := db.QueryRow(ctx, countRowsPrefix+target).Scan(&held); err != nil {
		return 0, finished(err)
	}
	return held, nil
}

// isPartitionOf answers whether one table is a partition of one parent, both inside the configured
// schema, from pg_inherits and never from the shape of its name. A name match is forgeable and
// coincidental, and it is the predicate that deletes a customer's table while passing every
// ordinary input (criterion 38, skill Pattern 7). A detached partition has no pg_inherits row --
// measured -- so an orphan still carrying our marker answers false, which is the case ADR-6 keeps
// the detach and the drop in one transaction for.
func isPartitionOf(ctx context.Context, db catalogReader, schema, partition, parent string) (bool, error) {
	var attached bool
	err := db.QueryRow(ctx, attachedPartitionQuery, schema, partition, parent).Scan(&attached)
	if err != nil {
		return false, finished(err)
	}
	return attached, nil
}

// dbNow is the instant every partition decision is taken against.
//
// now() is the transaction timestamp, which is what makes an observation and the decision taken on
// it agree when they share a transaction. Reading this process's clock instead passes every
// single-process test and fails only when two replicas disagree about which partitions must exist,
// at which point one creates what the other is dropping.
func dbNow(ctx context.Context, db catalogReader) (time.Time, error) {
	var reading time.Time
	if err := db.QueryRow(ctx, transactionNowQuery).Scan(&reading); err != nil {
		return time.Time{}, finished(err)
	}
	return reading, nil
}

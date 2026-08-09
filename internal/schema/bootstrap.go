package schema

import (
	"context"
	"fmt"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is the boot's spine: the eleven steps the task file's Workflow Steps table orders, each
// one call, composed out of advisorylock.go, catalog.go, ownershipmarker.go, runner.go, maintain.go
// and partition.go. Every decision it takes is one of theirs; what it owns is the *order*, which is
// the contract behind criteria 1, 2, 17, 18, 19, 20, 21 and 31. bootstrapschema.go is the other
// half -- steps 1, 4 and 5, which are refusals rather than sequence.
//
// The order is behaviour rather than description, and two inputs violating two steps at once are
// what make it so. A reserved schema name against a pool that can produce no connection pins step 1
// before step 2 (TestAReservedNameIsRefusedBeforeAConnectionThatCannotBeMade); the same name
// against a database already ahead of this binary pins step 1 before step 7
// (TestAReservedNameOnADatabaseAheadOfTheBinaryIsRefusedForTheName); and the same name beside a
// horizon no database can hold pins step 1 before the guard below it
// (TestAReservedNameOnAnUnservableHorizonIsRefusedForTheName). With every input violating one step
// only, both orders answer identically, and reversing them would change which reason an operator is
// shown while the suite stayed green.
//
// Two things this file deliberately does not do.
//
// It does not refuse to start over a DEFAULT partition holding rows in a range a pass wanted to
// create (ADR-8, Accepted). One stray row must never turn a degraded state into an outage: step 9's
// pass counts and logs that range, and RepairDefaultPartition drains it. Step 10 is the only thing
// here that can refuse over partitions, and it asks whether the *horizon* is covered rather than
// why any single range is not (criterion 31).
//
// It runs nothing on the pool after step 2. Everything from the lock to the unlock runs on the one
// pinned connection: ADR-4 requires that of the lock, and correctness requires it of the rest,
// because a customer configuring a pool of one connection has nothing left for a second handle to
// acquire -- so a maintenance pass handed the pool would block until the acquire deadline instead of
// booting (TestBootstrapCompletesOnAPoolThatCanServeOneConnection).

// Bootstrap brings one database up to the schema this binary embeds and leaves it either fully
// correct or untouched. It is safe to run from N replicas at once: step 3 serialises them on one
// advisory key, and a replica finding the work already done does none of it again (criteria 18, 19).
//
// The pool is the caller's. One connection is borrowed from it for the whole run and handed back.
func Bootstrap(ctx context.Context, pool *pgxpool.Pool, cfg config.Config) (err error) {
	// Step 1, before any statement and before any connection: it is the only thing closing its
	// class, because every configured name reaches DDL quoted and the server accepts a quoted
	// CREATE SCHEMA for a name whose prefix is not exactly lower-case (M8).
	if claimsReservedSchemaPrefix(cfg.Database.Schema) {
		return reservedSchemaName(cfg.Database.Schema)
	}

	// Also before any statement and before any connection: a configuration whose horizon demands more
	// partitions than a database can hold is refused here, rather than met at step 9 as the allocation
	// a maintenance pass makes before it issues anything (horizon.go's unservableHorizonIn). It runs
	// after the name check, so an input violating both is told about the name -- pinned by
	// TestAReservedNameOnAnUnservableHorizonIsRefusedForTheName.
	if unservable := unservableHorizonIn(cfg.Retention); unservable != nil {
		return finished(unservable)
	}

	// Step 2.
	pinned, err := pool.Acquire(ctx)
	if err != nil {
		return finished(err)
	}
	defer pinned.Release()

	// Step 3.
	held, err := acquireLock(ctx, pinned, lockKey(cfg.Database.Schema, cfg.Instance))
	if err != nil {
		return err
	}
	// Step 11, deferred so that every refusal below gives the lock back on its way out -- and on the
	// connection that took it, because heldLock will accept no other. Its own failure is reported,
	// but never in place of one the run found: a connection that could not carry the unlock usually
	// could not carry the work either, and the work's error is the one that says what went wrong.
	defer func() {
		if released := held.release(ctx); released != nil && err == nil {
			err = released
		}
	}()

	return boot{on: pinned, cfg: cfg, opts: Options{}.normalized()}.underTheLock(ctx)
}

// boot is one run's handle, configuration and options, gathered so that steps 4 to 10 read as the
// sequence they are rather than as three parameters threaded through each. on is the pinned
// connection and never the pool -- see the note at the top of this file.
type boot struct {
	on   *pgxpool.Conn
	cfg  config.Config
	opts Options
}

// underTheLock is steps 4 to 10, in the order Workflow Steps sets.
//
// Each call below has already finished its own error, and each is finished again here. That is a
// fixed point rather than a second elision -- the text and the sentinel both survive it -- and it
// keeps every path out of this file uniform, so no future step has to remember which of them was
// already finished (ADR-11).
func (run boot) underTheLock(ctx context.Context) error {
	// Steps 4 and 5.
	if err := run.claimAndCreateSchema(ctx); err != nil {
		return finished(err)
	}

	// Steps 6, 7 and 8: read the ledger, refuse a database ahead of this binary, apply what is
	// pending -- all three the runner's, reached through the entry point that defaults to the
	// embedded corpus (ADR-10) rather than by reaching for that corpus here.
	if _, err := migrateEmbedded(ctx, run.on, run.cfg.Database.Schema); err != nil {
		return finished(err)
	}

	// Step 9: one maintenance pass, on the same pinned connection.
	//
	// Its answer is deliberately not propagated. A refused range is non-fatal to the boot by the
	// workflow's own policy: the pass has already counted every refusal and logged it at the failure
	// level (criterion 33), and one range refused -- by a lock it could not take, or by rows already
	// sitting in DEFAULT -- must not stop a service from starting. What the boot gates on is step
	// 10, which asks the question that matters rather than the one that happened to be asked.
	_, _ = Maintain(ctx, run.on, run.cfg, run.opts)

	// Step 10.
	return run.refuseShortCoverage(ctx)
}

// coverageReading is the world step 10 decides against: the instant, and the bounded partitions
// observed at it.
type coverageReading struct {
	now     time.Time
	bounded []Range
}

// refuseShortCoverage is workflow step 10: a schema whose partitions already fall short of
// now + precreate is refused rather than booted into, because every write past the last covered
// range lands in DEFAULT (criterion 31).
func (run boot) refuseShortCoverage(ctx context.Context) error {
	seen, err := run.readCoverage(ctx)
	if err != nil {
		return finished(err)
	}
	return run.shortCoverageRefusal(seen)
}

// readCoverage reads that world inside one bounded transaction, so now() and the partitions are one
// reading rather than two instants. The instant is the database's and never this process's, because
// two replicas with skewed clocks must not disagree about which partitions have to exist (SC-6);
// the bound is there because deparsing a partition bound waits behind ACCESS EXCLUSIVE on the
// relation it describes (M5), and an unbounded wait here is a boot that hangs.
func (run boot) readCoverage(ctx context.Context) (coverageReading, error) {
	var seen coverageReading

	err := boundedTx(
		ctx, run.on, run.opts, ddlSubject{operation: observingWork},
		func(ctx context.Context, tx pgx.Tx) error {
			var err error
			if seen.now, err = dbNow(ctx, tx); err != nil {
				return err
			}
			found, err := observePartitions(ctx, tx, run.cfg.Database.Schema, TableEvents)
			seen.bounded = found.Bounded
			return err
		},
	)
	if err != nil {
		return coverageReading{}, finished(fmt.Errorf("%s: %w", observingWork, err))
	}
	return seen, nil
}

// shortCoverageRefusal is step 10's decision, and it is Step 3's arithmetic and no more of its own:
// the shortfall is CoverageShortfall's, and the refusal names it alongside the horizon it falls
// short of, so the gap is actionable without reading the configuration beside the message.
//
// It is separated from the read above because a live clock cannot be made to land on a boundary on
// demand, and criterion 31's decisive case is coverage reaching the horizon *exactly*. Pairing one
// real reading -- the database's own instant, the catalog's own bounds -- with each of the three
// horizons is the one thing TestCoverageIsRefusedOnlyWhenItFallsShortOfTheHorizon arranges.
func (run boot) shortCoverageRefusal(seen coverageReading) error {
	short := CoverageShortfall(seen.bounded, seen.now, run.cfg.Retention.Precreate)
	if short <= 0 {
		return nil
	}
	return finished(coverageShort(short, run.cfg.Retention.Precreate))
}

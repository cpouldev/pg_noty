package schema

import (
	"math"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
)

// This file is the one question asked of a configuration before anything else happens: can the
// pre-creation horizon it describes be served at all? plan.go answers what a pass would change; this
// answers whether there is any point in asking. Both entry points that own an error channel call
// unservableHorizonIn and nothing else here -- Step 13's Bootstrap before it takes a connection, and
// Step 11's Maintain before it reads the catalog.
//
// It split from plan.go when the second condition landed, at the seam the three test files had
// already been split along: what the count is (plancardinality_test.go), what the bounds are
// (horizonbound_test.go), and who asks (horizonrefusal_test.go).
//
// Every bound below is a property of PostgreSQL and not of one server, which is what lets this file
// stay as pure as plan.go: an oid is four unsigned bytes and a timestamptz keeps microseconds on
// every installation, so both are knowable before a connection exists. A bound that differed
// between a customer's cluster and the container these were measured on could not live here at all
// -- horizonlockcapacity_integration_test.go is where the one candidate for such a bound, the
// server's own lock table, was measured and found not to bind this package.

// relationIdentifiersOneDatabaseHolds is how many relations one PostgreSQL database can hold at
// once. Every partition is a relation, every relation carries a pg_class.oid, and an oid is an
// unsigned four-byte integer, so the whole space is 2^32 identifiers -- and a partition set larger
// than that cannot be realised on any server, whatever memory the process planning it has.
//
// The width is measured rather than transcribed: TestTheServerNamesRelationsInAThirtyTwoBitIdentifierSpace
// watches the server accept the largest oid and refuse the next one, so a release that widened the
// type fails there instead of leaving this bound quietly narrow.
const relationIdentifiersOneDatabaseHolds int64 = 1 << 32

// storedInstantGranularity is the resolution a timestamptz keeps, and therefore the unit every
// partition bound is rounded to when it is written. A bound is rendered at nanosecond precision
// (maintainreport.go's boundLiteral) and stored to the nearest microsecond, so a grid whose interval
// is not a whole multiple of this puts bounds between two stored instants and the catalog reports
// back an extent nobody asked for.
//
// It is measured rather than transcribed, in both directions:
// TestTheServerStoresAnInstantToTheMicrosecond watches two instants a nanosecond apart round-trip
// through timestamptz as one value and two a microsecond apart round-trip as two, and
// TestOnlyAWholeMultipleOfAStoredInstantSurvivesBeingWrittenAndReadBack watches the extents
// themselves survive on a multiple of it and change on every grid that is not one.
const storedInstantGranularity = time.Microsecond

// The two partitions a horizon demands past the whole intervals it spans, named so that the
// difference between them is a value rather than a literal in a branch. horizonPartitionCount
// derives both: one is always the partition `now` itself sits in, and the second is demanded only by
// a horizon that leaves a remainder.
const (
	partitionsBeyondAHorizonThatDivides   int64 = 1
	partitionsBeyondAHorizonWithRemainder int64 = 2
)

// unservableHorizonIn is the refusal a configuration whose pre-creation horizon no database can serve
// earns, or nil for one that can be served. It is a precondition on the configuration and reads no
// clock, so Bootstrap can ask it before it has a connection.
//
// Three conditions, in this order, and the order is behaviour rather than description. Each is
// asked before the one whose remedy it would invalidate, so every message is true of the
// configuration it was given and the sequence of remedies terminates on a servable one.
//
// The sign is asked first. A negative interval is usually also not a whole multiple of the stored
// instant -- `-1500ns` is both -- and an operator told about the granularity has a remedy, rounding
// to whole microseconds, that leaves the interval negative and the configuration exactly as
// unservable. Told about the sign, the remedy is a positive duration, which may then be unstorable
// and earns that message next.
// TestANegativeIntervalIsRefusedForItsSignAndNotItsGranularity is the input violating both that pins
// it.
//
// The unstorable interval is asked before the count on the same ground. Take `1ns` over a `48h`
// horizon, which violates both. Told about the count, an operator has two remedies and one of them
// is wrong: shortening the horizon to `1ms` brings the count to 1,000,001, under the bound, and
// leaves a configuration whose partitions this package can create and then never recognise again.
// They have been sent to fix the wrong thing, and the defect resurfaces as a boot that fails on
// every pass. Told about the interval, the only remedy is to round it up to whole microseconds,
// which is also the count's other remedy.
// TestAnIntervalFinerThanAStoredInstantIsRefusedForTheIntervalAndNotTheCount pins that order.
//
// The first condition tests divisibility and not size, and the difference is the whole class rather
// than the reproduction that found it. The reproduction was `1ns`, whose adjacent bounds round to
// one instant and earn an outright refusal; being *under* a microsecond is that reproduction's
// incidental property. Measured on 17.10, a `1500ns` grid clears any size test, is accepted by the
// server, and reads back as a `[.000002,.000003)` extent the arithmetic never asked for -- and this
// package decides what exists by comparing observed extents and never by name (M6), so such a
// partition is created once and re-requested forever. Every bound is a whole multiple of the
// interval counted from the epoch, which is itself a whole microsecond, so the extents survive
// exactly when the interval does.
//
// The identifier bound is `>=` rather than `>` because the partitions are never alone in the
// database: the parent they attach to and the permanent DEFAULT partition beside them carry
// identifiers of their own, so a plan asking for the whole space is already one too many.
//
// Why a precondition at all: RequiredRanges preallocates one Range per required partition, so a
// count discovered by building the slice is a count discovered too late. internal/config accepts a
// one-nanosecond partition_interval beside the 48h precreate its own defaults block writes -- R10
// requires only that both be positive and R12 only that the horizon cover one interval -- and that
// pair is refused here by the first condition, having asked for 172,800,000,000,001 partitions of
// which not one could have been created.
//
// Neither of those conditions changes what a *zero* interval does, and the sign is tested `< 0`
// rather than `<= 0` for that reason. R10 guarantees partition_interval greater than zero, so a
// zero one reaching here means the caller bypassed internal/config -- and zero divides, so it falls
// past both conditions to the same division it always did and panics there, on the stated ground
// that a set covering nothing would be a wrong answer where a panic is a loud one.
// TestAnIntervalR10RefusesPanicsInTheGuardJustAsItAlreadyDidInTheArithmetic asserts both halves of
// that and is left untouched.
//
// A negative interval is answered rather than left to panic the same way, because the two panics are
// not in the same place. Zero divides by zero *inside this guard*, so a boot dies at its
// precondition, before a connection exists. A negative one divides cleanly and clears every other
// condition -- 48h over -1h counts -47 partitions, well under the identifier bound -- so it reached
// RequiredRanges at step 9 instead, with the advisory lock held and every migration applied, where
// the k-range runs backwards and make panics on a negative capacity.
// TestANegativeIntervalIsRefusedWhereAZeroOneStillPanics holds the two apart.
//
// What this does not close, stated rather than left to be met: a count *below* the identifier bound
// can still be larger than the memory this process has -- 4,294,967,295 Ranges is hundreds of
// gigabytes -- and the line between "a server could hold it" and "this process can allocate it" is a
// memory budget no ADR states and no measurement fixes. Narrowing the bound is a policy decision, and
// this guard is where it would be made.
//
// One candidate for a tighter, server-derived bound was measured and left out, which is worth more
// written down than rediscovered. A query against a partitioned parent takes a lock per partition,
// and PostgreSQL sizes its lock table at startup from max_locks_per_transaction * (max_connections
// + max_prepared_transactions) -- so such a query has a partition ceiling, past which the server
// answers `out of shared memory` (SQLSTATE 53200). This package issues no such statement: every
// statement it aims at the event log takes the same number of locks at any partition count, because
// lockTheEventLog writes LOCK TABLE ONLY, catalog.go counts the DEFAULT partition by name rather
// than through the parent, and each create, detach, drop and drain names one partition. A lock
// bound here would refuse configurations this package serves perfectly well, and
// TestEveryStatementAimedAtTheEventLogTakesTheSameLocksAtAnyPartitionCount is what keeps the reason
// it is absent true -- each of those three properties is one word away from being false.
func unservableHorizonIn(retention config.Retention) error {
	if retention.PartitionInterval < 0 {
		return negativeInterval(retention.PartitionInterval)
	}
	if retention.PartitionInterval%storedInstantGranularity != 0 {
		return unstorableInterval(retention.PartitionInterval)
	}

	count := horizonPartitionCount(retention)
	if count < relationIdentifiersOneDatabaseHolds {
		return nil
	}
	return unservableHorizon(count, retention.PartitionInterval, retention.Precreate)
}

// horizonPartitionCount is how many partitions the configured pre-creation horizon demands, computed
// from the configuration alone and without building one of them.
//
// It is the largest count any instant can demand, which is what a check made before the database's
// clock is read has to be. RequiredRanges' k-range is inclusive at both ends (ADR-6), so writing
// now = a*interval + b and precreate = whole*interval + remainder, with 0 <= b < interval and
// 0 <= remainder < interval, the count is whole + floor((b+remainder)/interval) + 1. Since
// b + remainder < 2*interval that floor is 0 or 1, and it is 0 for every b when the remainder is 0,
// because b alone is under one interval. So a horizon that divides demands exactly whole + 1 and one
// that does not demands whole + 1 or whole + 2, depending on where inside its interval `now` sits.
// TestTheHorizonCountIsWhatRequiredRangesBuildsAtItsWorstPhase reconciles both against the ranges
// actually built.
//
// It saturates rather than wrapping, and a wrapped count would read as a small one and be admitted,
// which is the one outcome this arithmetic exists to prevent. No input unservableHorizonIn passes on
// can reach the saturation any more -- an interval of at least a microsecond leaves whole at most
// MaxInt64/1000 -- but that is a fact about *that caller's* filtered input and not about this
// function's own domain, which is every retention R10 admits. The row named for the largest horizon
// a time.Duration expresses in horizonGuardRows reaches it directly.
//
// PartitionInterval is greater than zero, which R10 guarantees. A zero one divides by zero and
// panics here, deliberately and for the reason RequiredRanges gives for the same division. A
// negative one is answered by unservableHorizonIn's first condition before this is reached, so the
// sign is not re-tested here.
func horizonPartitionCount(retention config.Retention) int64 {
	whole := int64(retention.Precreate / retention.PartitionInterval)

	beyond := partitionsBeyondAHorizonWithRemainder
	if retention.Precreate%retention.PartitionInterval == 0 {
		beyond = partitionsBeyondAHorizonThatDivides
	}

	if whole > math.MaxInt64-beyond {
		return math.MaxInt64
	}
	return whole + beyond
}

package schema

import (
	"testing"
	"time"
)

// This file is the cardinality of the set a configuration asks for: the arithmetic that counts it
// without building one of them. horizonbound_test.go is the pair of bounds that count is refused at,
// horizonrefusal_test.go the two entry points that ask, and horizonprecedence_test.go which reason
// an input breaking two rules is told about.
//
// It carried a recorded obligation until the horizon guard landed, and that obligation is retired
// here rather than left standing. The fact it recorded is unchanged: R10 requires only that
// partition_interval and precreate be positive and R12 only that precreate cover one interval, so
// `partition_interval: 1ns` beside the `precreate: 48h` internal/config's own defaults block writes
// validates cleanly and asks for 172,800,000,000,001 partitions -- which RequiredRanges
// preallocates one Range apiece for. Measured on darwin/arm64, go1.25: a Range is 64 bytes, that
// preallocation asks the runtime for 11,059,200,000,000,064 of them, and it dies as `panic: runtime
// error: makeslice: cap out of range`. A count small enough to sit under the runtime's maximum
// allocation size and still over the machine's memory dies instead as the unrecoverable `runtime:
// out of memory` the obligation named; the two are one defect met at two sizes. What changed is
// that horizon.go's unservableHorizonIn now refuses it, before a connection is taken and before a
// Range is built.
//
// That configuration is now refused by the *granularity* condition rather than by the count, and the
// distinction is worth keeping straight: at a nanosecond interval the count is beside the point,
// because not one of those 172,800,000,000,001 partitions could have been created either -- their
// bounds are two names for one stored instant. The count condition owns the configurations whose
// ranges the server would accept one at a time and cannot hold together.
//
// Both bounds are derived and not chosen, which is what the two steps that declined this obligation
// were right to insist on, and both are measured against the running server rather than transcribed:
// TestTheServerNamesRelationsInAThirtyTwoBitIdentifierSpace for the oid width, and
// TestTheServerStoresAnInstantToTheMicrosecond for the granularity.
//
// What they do *not* close, said here so it is not mistaken for closed: a count below 2^32 can still
// be more than this process can allocate -- 4,294,967,295 Ranges is hundreds of gigabytes -- and the
// line between "a server could hold it" and "this process can allocate it" is a memory budget no ADR
// states and no measurement fixes. Narrowing the bound to close that gap is a policy decision, and
// unservableHorizonIn is where it would be made.
//
// The one candidate for a tighter *server-derived* bound was measured and rejected. PostgreSQL sizes
// its lock table from max_locks_per_transaction * (max_connections + max_prepared_transactions), and
// a query against a partitioned parent takes a lock per partition -- so such a query has a partition
// ceiling. This package issues none: measured at 51, 401 and 3,000 partitions against a server whose
// whole lock table held 200 entries, its statements took the same lock counts every time. The
// standing assertion of that is
// TestEveryStatementAimedAtTheEventLogTakesTheSameLocksAtAnyPartitionCount.

// horizonPhase is one instant inside a partition interval, offset from a grid line, and the number
// of ranges RequiredRanges builds when `now` sits there. Every number is counted from the k-range
// rather than read back from a run.
type horizonPhase struct {
	offset time.Duration
	ranges int
}

// horizonReconciliations pairs the now-free count with what RequiredRanges actually builds at each
// phase of `now` inside one interval. thePlanNow is a whole number of seconds after the epoch, so it
// sits exactly on a grid line for every interval below and each offset *is* the b of the derivation
// at horizonPartitionCount.
var horizonReconciliations = []struct {
	name                string
	interval, precreate time.Duration
	phases              []horizonPhase
	// want is the largest of the phases: whole+1 for a horizon that divides, whole+2 for one that
	// does not.
	want int64
}{
	{
		// whole = 2, remainder = 0. floor((b+48h)/24h) is 2 for every b under 24h, so the count is
		// 3 at every phase and the horizon's own divisibility is what makes it flat.
		name: "a horizon of two whole intervals", interval: thePlanInterval, precreate: thePlanPrecreate,
		want: 3,
		phases: []horizonPhase{
			{offset: 0, ranges: 3},
			{offset: time.Nanosecond, ranges: 3},
			{offset: thePlanInterval - time.Nanosecond, ranges: 3},
		},
	},
	{
		// whole = 1, remainder = 6h. floor((b+6h)/24h) turns from 0 to 1 exactly at b = 18h, so the
		// count is 2 below that instant and 3 at it -- and the pair either side of 18h is the
		// equality case of that turn.
		name:     "a horizon that is not a whole multiple of the interval",
		interval: 24 * time.Hour, precreate: 30 * time.Hour, want: 3,
		phases: []horizonPhase{
			{offset: 0, ranges: 2},
			{offset: time.Nanosecond, ranges: 2},
			{offset: 18*time.Hour - time.Nanosecond, ranges: 2},
			{offset: 18 * time.Hour, ranges: 3},
			{offset: 24*time.Hour - time.Nanosecond, ranges: 3},
		},
	},
	{
		// whole = 1, remainder = 0: the partition now sits in, and the one the horizon starts.
		name:     "the smallest horizon R12 permits, one whole interval",
		interval: time.Hour, precreate: time.Hour, want: 2,
		phases: []horizonPhase{
			{offset: 0, ranges: 2},
			{offset: time.Hour - time.Nanosecond, ranges: 2},
		},
	},
	{
		// whole = 1000, remainder = 0: a second holds a thousand whole milliseconds.
		name:     "an interval finer than a second",
		interval: time.Millisecond, precreate: time.Second, want: 1001,
		phases: []horizonPhase{
			{offset: 0, ranges: 1001},
			{offset: time.Millisecond - time.Nanosecond, ranges: 1001},
		},
	},
}

// TestTheHorizonCountIsWhatRequiredRangesBuildsAtItsWorstPhase is what makes the now-free count
// usable before the database's clock is read. Both halves are asserted: every phase builds the
// number derived for it, and the largest of them is the count exactly -- an over-estimate would
// refuse configurations a server could serve, and an under-estimate would admit one it could not.
func TestTheHorizonCountIsWhatRequiredRangesBuildsAtItsWorstPhase(t *testing.T) {
	for _, tc := range horizonReconciliations {
		t.Run(tc.name, func(t *testing.T) {
			retention := retentionOf(tc.interval, tc.precreate, thePlanKeep)

			if counted := horizonPartitionCount(retention); counted != tc.want {
				t.Fatalf("the horizon count is %d, want the %d derived from the k-range; the number "+
					"the guard reads is no longer the number that would be allocated", counted, tc.want)
			}

			worst := 0
			for _, phase := range tc.phases {
				built := len(RequiredRanges(thePlanNow.Add(phase.offset), retention))
				if built != phase.ranges {
					t.Errorf("at %s past a grid line RequiredRanges builds %d ranges, want %d",
						phase.offset, built, phase.ranges)
				}
				worst = max(worst, built)
			}
			if int64(worst) != tc.want {
				t.Errorf("the worst phase builds %d ranges and the guard reads %d; a count above "+
					"what any instant demands refuses a servable configuration, and one below it "+
					"admits an unservable one", worst, tc.want)
			}
		})
	}
}

// horizonCountSink keeps the compiler from eliding the call the allocation measurement is about.
var horizonCountSink int64

// TestTheHorizonCountBuildsNoRangeToCountThem is the property the whole guard rests on: a refusal
// that had to build the set in order to discover it was too large would be the allocation it exists
// to prevent. Zero rather than a small number, because the arithmetic is a division, a remainder and
// a comparison on int64, and nothing there has anywhere to allocate. It is measured over the
// configuration the guard exists for rather than a servable one, so the input is the one a reader
// would ask about.
func TestTheHorizonCountBuildsNoRangeToCountThem(t *testing.T) {
	retention := retentionOf(time.Nanosecond, 48*time.Hour, thePlanKeep)

	allocations := testing.AllocsPerRun(100, func() {
		horizonCountSink = horizonPartitionCount(retention)
	})

	if allocations != 0 {
		t.Errorf("counting the %d partitions that configuration asks for allocated %v times per "+
			"run, want none: a count that allocates is a count taken too late",
			horizonCountSink, allocations)
	}
}

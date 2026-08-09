package schema

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
)

// This file is the two bounds the horizon guard refuses at, and the configurations they are crossed
// with. plancardinality_test.go holds the count one of them compares, horizonrefusal_test.go the
// two entry points that ask, and horizonprecedence_test.go the inputs that violate two rules at
// once. The four split where the 200-line budget this package's own gate put them, and the seams
// are the questions: what the count is, what the bounds are, who asks, and which reason wins.

// TestTheHorizonBoundIsTheRelationIdentifierSpace pins the constant against the literal the measured
// fact produces, rather than against whatever the guard happens to compare with. Its twin against
// the running server is TestTheServerNamesRelationsInAThirtyTwoBitIdentifierSpace, which is where an
// oid that stopped being four unsigned bytes fails.
func TestTheHorizonBoundIsTheRelationIdentifierSpace(t *testing.T) {
	const anUnsignedFourByteSpace int64 = 4294967296

	if relationIdentifiersOneDatabaseHolds != anUnsignedFourByteSpace {
		t.Errorf(
			"the bound is %d, want the %d identifiers an unsigned four-byte oid expresses; a "+
				"bound that is not the identifier space is a number somebody picked",
			relationIdentifiersOneDatabaseHolds, anUnsignedFourByteSpace,
		)
	}
}

// TestTheStoredInstantGranularityIsAMicrosecond is the same pin for the second bound. PostgreSQL
// documents timestamptz as holding microsecond resolution, so the constant is asserted against that
// literal and not against the guard that reads it. Its twin against the running server is
// TestTheServerStoresAnInstantToTheMicrosecond.
func TestTheStoredInstantGranularityIsAMicrosecond(t *testing.T) {
	if storedInstantGranularity != time.Microsecond {
		t.Errorf(
			"the granularity is %s, want the %s a timestamptz stores; a granularity that is not "+
				"the type's own is a number somebody picked", storedInstantGranularity, time.Microsecond,
		)
	}
}

// TestTheHorizonGuardRefusesEachConditionAtItsOwnBoundAndNotBefore crosses both bounds in both
// directions with the equality case on each, and carries the large-but-servable rows an over-eager
// guard would refuse. Every row is a configuration internal/config accepts, which the loop asserts
// rather than assumes -- a row internal/config rejects proves nothing.
//
// The expected refusal is compared by equality against the constructor rather than by sentinel,
// because both conditions report under ErrUnservableHorizon and only the wording keeps them
// apart.
func TestTheHorizonGuardRefusesEachConditionAtItsOwnBoundAndNotBefore(t *testing.T) {
	for _, tc := range horizonGuardRows {
		t.Run(
			tc.name, func(t *testing.T) {
				retention := retentionOf(tc.interval, tc.precreate, thePlanKeep)
				assertConfigAcceptsTheRetention(t, retention)

				if counted := horizonPartitionCount(retention); counted != tc.wantCount {
					t.Fatalf(
						"a %s interval over a %s horizon counts %d partitions, want the %d derived "+
							"beside the row", tc.interval, tc.precreate, counted, tc.wantCount,
					)
				}
				assertRefusedAs(t, unservableHorizonIn(retention), tc.want)
			},
		)
	}
}

// assertRefusedAs checks the guard's answer against the one refusal the row derives, clause by
// clause, so an answer missing one fails the clause named for it. The sentinel is asserted beside
// the wording rather than through it: a constructor that stopped wrapping would render identical
// text and leave every caller unable to classify what it got.
func assertRefusedAs(t *testing.T, answered, want error) {
	t.Helper()

	switch {
	case want == nil && answered != nil:
		t.Fatalf("a configuration the guard must serve was refused as %v", answered)
	case want == nil:
		return
	case answered == nil:
		t.Fatalf("the guard served a configuration it must refuse as %q", want)
	case answered.Error() != want.Error():
		t.Errorf(
			"refused as %q, want %q; the two conditions have two remedies and the wording is "+
				"the only thing keeping them apart", answered, want,
		)
	}
	if !errors.Is(answered, ErrUnservableHorizon) {
		t.Errorf(
			"the refusal %v is not %v, so a caller cannot tell it from any other boot failure",
			answered, ErrUnservableHorizon,
		)
	}
}

// assertConfigAcceptsTheRetention is R10, R11 and R12 read off the value the guard is handed, so a
// row describing a configuration internal/config would reject cannot stand in for one it accepts.
// All three rules, and not only the two the guard reads: the horizon is asserted against
// configurations a customer can actually write, and keep is one of the three fields that decides
// whether they can.
func assertConfigAcceptsTheRetention(t *testing.T, retention config.Retention) {
	t.Helper()

	if retention.PartitionInterval <= 0 || retention.Precreate <= 0 || retention.Keep <= 0 {
		t.Fatalf(
			"retention %+v: R10 requires all three positive, so this row does not describe a "+
				"configuration internal/config accepts", retention,
		)
	}
	if retention.Precreate < retention.PartitionInterval {
		t.Fatalf(
			"precreate %s is under one interval %s, which R12 refuses",
			retention.Precreate, retention.PartitionInterval,
		)
	}
	if retention.Keep < retention.PartitionInterval {
		t.Fatalf(
			"keep %s is under one interval %s, which R11 refuses",
			retention.Keep, retention.PartitionInterval,
		)
	}
}

// horizonGuardRows are the configurations the two bounds are asserted through: servable ones an
// over-eager guard would refuse, the pair either side of each bound with the equality case on it,
// the reproduction this file recorded as an obligation, and the horizon whose arithmetic would wrap.
//
// Every count is derived from the k-range beside the row it belongs to and never read back from a
// run.
var horizonGuardRows = []struct {
	name                string
	interval, precreate time.Duration
	wantCount           int64
	// want is the refusal this row earns, or nil for a configuration the guard must serve.
	want error
}{
	// 168h holds 7 whole days, and the k-range is inclusive at both ends: the 8 partitions the
	// alignment rule names for internal/config's own defaults, beside the permanent DEFAULT.
	{
		name:     "internal/config's defaults, a week of daily partitions",
		interval: 24 * time.Hour, precreate: 168 * time.Hour, wantCount: 8,
	},
	// 8760 whole hours in a 365-day year, plus the one now sits in.
	{
		name:     "a year of hourly partitions, far larger than any deployment needs",
		interval: time.Hour, precreate: 8760 * time.Hour, wantCount: 8761,
	},
	// 168h is 604,800 seconds. Over half a million partitions is a configuration nobody should
	// write, and it is not this guard's business to say so.
	{
		name:     "a week of one-second partitions, large and still servable",
		interval: time.Second, precreate: 168 * time.Hour, wantCount: 604801,
	},

	// The granularity condition, crossed both ways. The interval that *is* a stored instant: a second
	// holds 1,000,000 whole microseconds and divides exactly, so the count is one more.
	{
		name:     "an interval of exactly the instant a timestamptz stores",
		interval: time.Microsecond, precreate: time.Second, wantCount: 1000001,
	},
	// A whole multiple that is not the granularity itself, so a condition written `== 1µs` fails
	// here. 500,000 whole intervals, and the k-range is inclusive at both ends.
	{
		name:     "an interval of two whole stored instants",
		interval: 2 * time.Microsecond, precreate: time.Second, wantCount: 500001,
	},
	// One nanosecond finer than a stored instant, and nothing else changed. 999ns divides
	// 1,000,000,000ns 1,001,001 times with 1ns left over, so the remainder adds two rather than one.
	{
		name:     "an interval one nanosecond finer than a stored instant",
		interval: 999 * time.Nanosecond, precreate: time.Second, wantCount: 1001003,
		want: unstorableInterval(999 * time.Nanosecond),
	},
	// The class case, differing from the row above only in the property that was incidental to it: this
	// interval is *coarser* than a stored instant and still not a whole multiple of one, so a condition
	// testing size rather than divisibility serves it and the partitions it names are created once and
	// never recognised again. 1,500ns divides 1,000,000,000ns 666,666 times with 1,000ns left over, so
	// the remainder adds two.
	{
		name:     "an interval coarser than a stored instant and not a whole multiple of one",
		interval: 1500 * time.Nanosecond, precreate: time.Second, wantCount: 666668,
		want: unstorableInterval(1500 * time.Nanosecond),
	},

	// The identifier bound, approached on a one-second grid so the whole intervals are the seconds.
	// 4,294,967,294 divides exactly, so the count is one more: 4,294,967,295, one short of the space.
	{
		name:     "one partition short of the identifier space",
		interval: time.Second, precreate: 4294967294 * time.Second, wantCount: 4294967295,
	},
	{
		name:     "exactly the identifier space",
		interval: time.Second, precreate: 4294967295 * time.Second, wantCount: 4294967296,
		want: unservableHorizon(4294967296, time.Second, 4294967295*time.Second),
	},
	// The same count reached the other way: one whole interval fewer, and a remainder, which adds
	// two rather than one. Only a count that reads the remainder answers 4,294,967,296 here.
	{
		name:     "exactly the identifier space, reached by a horizon that leaves a remainder",
		interval: time.Second, precreate: 4294967294*time.Second + time.Nanosecond,
		wantCount: 4294967296,
		want:      unservableHorizon(4294967296, time.Second, 4294967294*time.Second+time.Nanosecond),
	},
	// The identifier bound reached on the finest grid a timestamptz stores, so the interval sits
	// exactly on the *other* bound and passes it. Without this row the count condition is only ever
	// reached at intervals far from the granularity one, and a granularity test written `<=` would
	// shadow it here with nothing failing.
	{
		name:     "exactly the identifier space, on an interval of exactly a stored instant",
		interval: time.Microsecond, precreate: 4294967295 * time.Microsecond, wantCount: 4294967296,
		want: unservableHorizon(4294967296, time.Microsecond, 4294967295*time.Microsecond),
	},

	// The recorded obligation, which violates both rules. 48h is 172,800 seconds, so a one-nanosecond
	// grid puts the horizon 172,800,000,000,000 intervals away and the k-range is inclusive at both
	// ends. The count is over the identifier bound and the interval is under the granularity one, and
	// the documented order answers the interval.
	{
		name:     "internal/config's finest interval beside the horizon its defaults block writes",
		interval: time.Nanosecond, precreate: 48 * time.Hour, wantCount: 172_800_000_000_001,
		want: unstorableInterval(time.Nanosecond),
	},

	// The whole intervals are already math.MaxInt64 here, so anything added to them wraps -- and a
	// wrapped count reads as a small one and would be admitted. The count is asserted above whatever
	// the guard answers, which is what keeps the saturation pinned now that the granularity condition
	// refuses this configuration before the comparison is ever reached.
	{
		name:     "the largest horizon a time.Duration expresses, at the finest interval R10 permits",
		interval: time.Nanosecond, precreate: math.MaxInt64, wantCount: math.MaxInt64,
		want: unstorableInterval(time.Nanosecond),
	},
}

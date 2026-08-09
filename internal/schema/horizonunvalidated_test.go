package schema

import (
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
)

// This file is what this package does with a partition_interval internal/config would never have let
// through. R10 requires the interval to be greater than zero, so nothing here is reachable from a
// validated configuration -- which is exactly why the behaviour has to be pinned rather than left
// to be discovered.
//
// Two of them, and they are answered differently: zero panics at the guard and a negative one is
// refused by it. The second case is the reason the difference is worth two tests rather than one
// -- a negative interval used to be *served* here, and the panic it caused landed at step 9.
//
// It split from plancardinality_test.go, which owns the arithmetic over the intervals R10 does
// admit, when the 200-line budget this package's own gate enforces put them apart.

// TestAnIntervalR10RefusesPanicsInTheGuardJustAsItAlreadyDidInTheArithmetic pins what the guard does
// to a value internal/config cannot produce. R10 guarantees partition_interval greater than zero
// and this package may rely on that rather than re-checking it, so a zero one reaching here means
// the caller bypassed internal/config -- and RequiredRanges already answers that by dividing and
// panicking, on the stated ground that a set covering nothing would be a wrong answer rather than a
// loud one.
//
// Both halves are asserted, because the claim is that the guard moved *where* that panic happens
// and not *whether* it happens: earlier, from a configuration read before any statement, rather
// than at step 9 with the lock held and every migration already applied. A guard that started
// answering instead would fail the first half, and an arithmetic that stopped panicking the
// second.
//
// The guard's unstorable-interval condition is deliberately no threat to this claim. That condition
// asks whether the interval is a whole multiple of the microsecond a timestamptz stores, and zero
// is -- so a zero interval falls past it to the same division it always reached, and this pin means
// in the divisibility form exactly what it meant before it landed (horizon.go).
func TestAnIntervalR10RefusesPanicsInTheGuardJustAsItAlreadyDidInTheArithmetic(t *testing.T) {
	unvalidated := config.Retention{PartitionInterval: 0, Precreate: 48 * time.Hour, Keep: 48 * time.Hour}

	for _, tc := range []struct {
		name string
		call func()
	}{
		{name: "the guard the boot asks first", call: func() { _ = unservableHorizonIn(unvalidated) }},
		{name: "the arithmetic that already did", call: func() { _ = RequiredRanges(thePlanNow, unvalidated) }},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				if recovered := panicOf(tc.call); recovered == nil {
					t.Errorf(
						"a zero partition_interval answered rather than panicking; a set covering "+
							"nothing is a wrong answer where a panic is a loud one (%v)", recovered,
					)
				}
			},
		)
	}
}

// TestANegativeIntervalIsRefusedWhereAZeroOneStillPanics is the other half of the same claim, and
// the two halves are the reason the guard tests the sign `< 0` rather than `<= 0`.
//
// A negative interval divides the stored-instant granularity as often as not and counts a *negative*
// number of partitions, which is under the identifier bound -- so before this condition landed the
// guard served it and the failure arrived at step 9 instead, inside RequiredRanges, with the
// advisory lock held and every migration applied. The second half below is that failure, reached
// directly: it is what the refusal now stands in front of, and asserting the refusal without it
// would leave "the guard answers" indistinguishable from "there was nothing to answer".
func TestANegativeIntervalIsRefusedWhereAZeroOneStillPanics(t *testing.T) {
	const backwards = -time.Hour
	unvalidated := config.Retention{
		PartitionInterval: backwards, Precreate: 48 * time.Hour,
		Keep: 48 * time.Hour,
	}

	// -1h divides 1µs exactly and asks for -47 partitions, so neither of the other two conditions
	// can fire and this row reaches the one it is named for.
	if remainder := backwards % storedInstantGranularity; remainder != 0 {
		t.Fatalf(
			"%s leaves %s of a stored instant, so the unstorable condition answers this row and "+
				"the sign condition is never reached", backwards, remainder,
		)
	}
	if counted := horizonPartitionCount(unvalidated); counted >= relationIdentifiersOneDatabaseHolds {
		t.Fatalf(
			"a %s interval counts %d partitions, over the %d bound, so the count condition "+
				"could answer this row", backwards, counted, relationIdentifiersOneDatabaseHolds,
		)
	}

	assertRefusedAs(t, unservableHorizonIn(unvalidated), negativeInterval(backwards))

	if recovered := panicOf(func() { _ = RequiredRanges(thePlanNow, unvalidated) }); recovered == nil {
		t.Error(
			"RequiredRanges answered a negative interval rather than panicking, so the guard " +
				"above is refusing a configuration the arithmetic would have served and the refusal is " +
				"the wrong answer rather than an early one",
		)
	}
}

// panicOf is what one call panicked with, or nil for one that returned.
func panicOf(call func()) (recovered any) {
	defer func() { recovered = recover() }()
	call()
	return nil
}

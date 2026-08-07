package schema

import (
	"errors"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
)

// This file is the inputs that break two rules at once, and which reason each is told about. Every
// case establishes both violations on their own first: without that, the deciding input would break
// one rule only, both orders would answer identically, and reversing them would change which reason
// an operator is shown while the suite stayed green.
//
// Three orders are pinned here, and they are documented in two places. Step 1 before the horizon
// guard is bootstrap.go's; the sign before the unstorable interval, and the unstorable interval
// before the count, are horizon.go's.

// aNegativeButOtherwiseStorableInterval breaks the sign bound alone: -1µs divides the stored instant
// exactly, and a negative interval counts a negative number of partitions, which is under the
// identifier bound.
var aNegativeButOtherwiseStorableInterval = config.Retention{
	PartitionInterval: -time.Microsecond,
	Precreate:         48 * time.Hour, Keep: 72 * time.Hour,
}

// aPositiveUnstorableInterval breaks the granularity bound alone: 1,500ns is coarser than a stored
// instant, is not a whole multiple of one, and asks for far fewer partitions than the space holds.
var aPositiveUnstorableInterval = config.Retention{
	PartitionInterval: 1500 * time.Nanosecond,
	Precreate:         time.Second, Keep: 72 * time.Hour,
}

// aNegativeUnstorableInterval breaks both: -1,500ns is negative and leaves -500ns of a stored
// instant, so only the documented order answers the sign.
var aNegativeUnstorableInterval = config.Retention{
	PartitionInterval: -1500 * time.Nanosecond,
	Precreate:         48 * time.Hour, Keep: 72 * time.Hour,
}

// TestANegativeIntervalIsRefusedForItsSignAndNotItsGranularity pins horizon.go's first order. The
// sign is reported because it is the violation whose remedy the other one's would not survive: an
// operator told to round the interval up to whole microseconds writes -2µs and is no better off.
func TestANegativeIntervalIsRefusedForItsSignAndNotItsGranularity(t *testing.T) {
	onlyTheSign := unservableHorizonIn(aNegativeButOtherwiseStorableInterval)
	if want := negativeInterval(-time.Microsecond); onlyTheSign == nil ||
		onlyTheSign.Error() != want.Error() {
		t.Fatalf("the sign alone answered %v, want %q", onlyTheSign, want)
	}
	onlyTheGranularity := unservableHorizonIn(aPositiveUnstorableInterval)
	if want := unstorableInterval(1500 * time.Nanosecond); onlyTheGranularity == nil ||
		onlyTheGranularity.Error() != want.Error() {
		t.Fatalf(
			"the granularity alone answered %v, want %q; without a granularity violation that "+
				"really fires, the input below pins no order", onlyTheGranularity, want,
		)
	}

	both := unservableHorizonIn(aNegativeUnstorableInterval)

	if want := negativeInterval(-1500 * time.Nanosecond); both == nil || both.Error() != want.Error() {
		t.Errorf("an input breaking both rules was refused as %v, want the sign answer %q", both, want)
	}
	interval := aNegativeUnstorableInterval.PartitionInterval
	if interval%storedInstantGranularity == 0 {
		t.Errorf(
			"the deciding input's %s interval is a whole multiple of the %s a timestamptz "+
				"stores, so it breaks the granularity rule not at all and this case pins nothing",
			interval, storedInstantGranularity,
		)
	}
}

// aStorableButOverNumerousHorizon breaks the identifier bound alone: 4,294,967,295 whole microseconds
// divide exactly, so the count is one more -- the space exactly -- and the interval is exactly the
// instant a timestamptz stores, which clears the granularity condition.
var aStorableButOverNumerousHorizon = config.Retention{
	PartitionInterval: time.Microsecond,
	Precreate:         4_294_967_295 * time.Microsecond, Keep: 72 * time.Hour,
}

// anUnstorableIntervalOverAShortHorizon breaks the granularity bound alone: a microsecond of horizon
// on a nanosecond grid is 1,000 whole intervals and the k-range is inclusive at both ends, so 1,001
// partitions -- far under the identifier space.
var anUnstorableIntervalOverAShortHorizon = config.Retention{
	PartitionInterval: time.Nanosecond,
	Precreate:         time.Microsecond, Keep: 72 * time.Hour,
}

// anUnstorableIntervalOverALongHorizon breaks both: the recorded obligation, 172,800,000,000,001
// partitions on a grid no bound of which the server can tell from the next.
var anUnstorableIntervalOverALongHorizon = config.Retention{
	PartitionInterval: time.Nanosecond,
	Precreate:         48 * time.Hour, Keep: 48 * time.Hour,
}

// TestAnIntervalFinerThanAStoredInstantIsRefusedForTheIntervalAndNotTheCount pins horizon.go's
// order. The interval is reported because it is the violation whose remedy is necessary: an operator
// told about the count may shorten the horizon, which brings the count under the bound and leaves a
// configuration from which not one partition can be created.
func TestAnIntervalFinerThanAStoredInstantIsRefusedForTheIntervalAndNotTheCount(t *testing.T) {
	onlyTheCount := unservableHorizonIn(aStorableButOverNumerousHorizon)
	if want := unservableHorizon(
		4_294_967_296, time.Microsecond,
		4_294_967_295*time.Microsecond,
	); onlyTheCount == nil || onlyTheCount.Error() != want.Error() {
		t.Fatalf(
			"the count alone answered %v, want %q; without a count violation that really fires, "+
				"the input below pins no order", onlyTheCount, want,
		)
	}
	onlyTheInterval := unservableHorizonIn(anUnstorableIntervalOverAShortHorizon)
	if want := unstorableInterval(time.Nanosecond); onlyTheInterval == nil ||
		onlyTheInterval.Error() != want.Error() {
		t.Fatalf("the interval alone answered %v, want %q", onlyTheInterval, want)
	}

	both := unservableHorizonIn(anUnstorableIntervalOverALongHorizon)

	if want := unstorableInterval(time.Nanosecond); both == nil || both.Error() != want.Error() {
		t.Errorf(
			"an input breaking both rules was refused as %v, want the interval answer %q",
			both, want,
		)
	}
	if counted := horizonPartitionCount(anUnstorableIntervalOverALongHorizon); counted <
		relationIdentifiersOneDatabaseHolds {
		t.Errorf(
			"the deciding input asks for %d partitions, under the %d bound, so it breaks the "+
				"count rule not at all and this case pins nothing",
			counted, relationIdentifiersOneDatabaseHolds,
		)
	}
}

// TestAReservedNameOnAnUnservableHorizonIsRefusedForTheName pins bootstrap.go's order: step 1 runs
// before the horizon guard below it, so a configuration breaking both is told about the name.
func TestAReservedNameOnAnUnservableHorizonIsRefusedForTheName(t *testing.T) {
	const reserved = "pg_noty"

	usableName := unservableConfiguration(anUnstorableIntervalOverALongHorizon)
	if atTheHorizon := Bootstrap(t.Context(), nil, usableName); !errors.Is(atTheHorizon, ErrUnservableHorizon) {
		t.Fatalf(
			"the horizon alone answered %v, want %v; without a second violation that really "+
				"fires, the input below pins no order", atTheHorizon, ErrUnservableHorizon,
		)
	}
	servableName := configWithSchema(reserved)
	if atTheName := Bootstrap(t.Context(), nil, servableName); atTheName == nil {
		t.Fatal("the reserved name alone answered no error")
	}

	both := unservableConfiguration(anUnstorableIntervalOverALongHorizon)
	both.Database.Schema, both.Instance = reserved, reserved
	answered := Bootstrap(t.Context(), nil, both)

	if want := reservedSchemaName(reserved).Error(); answered == nil || answered.Error() != want {
		t.Errorf(
			"an input breaking both rules was refused as %v, want the name answer %q",
			answered, want,
		)
	}
	if errors.Is(answered, ErrUnservableHorizon) {
		t.Error(
			"the boot reported the horizon rather than the schema name, so the order is " +
				"reversed and an operator is sent to fix the wrong thing",
		)
	}
}

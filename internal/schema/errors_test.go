package schema

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestTheSentinelVocabularyIsPinnedAtSeven keeps the set the distinguishability sweep quantifies
// over from shrinking silently. Each member is asserted against by name somewhere, so an eighth
// sentinel has to join the set rather than merely join the file. It read six until the horizon
// guard landed ErrUnservableHorizon, which is the seventh errors.go's own note anticipated.
func TestTheSentinelVocabularyIsPinnedAtEight(t *testing.T) {
	if len(packageSentinels) != 8 {
		t.Fatalf("the vocabulary declares %d sentinels %v; update this count with the set",
			len(packageSentinels), packageSentinels)
	}
	for _, declared := range []error{
		ErrSchemaAhead, ErrCoverageShort, ErrDefaultBlocked, ErrLockTimeout,
		ErrForeignInstance, ErrPrivilege, ErrUnservableHorizon, ErrLedgerAbsent,
	} {
		if !containsSentinel(packageSentinels, declared) {
			t.Errorf("%v is exported and not in the vocabulary, so the finishing point cannot "+
				"carry it and the distinguishability sweep never sees it", declared)
		}
	}
}

// containsSentinel is membership by identity rather than by errors.Is, because the question here is
// whether the set holds this very sentinel and errors.Is would answer yes for a wrapper of it.
func containsSentinel(set []error, want error) bool {
	for _, member := range set {
		if member == want {
			return true
		}
	}
	return false
}

// TestEverySentinelIsDistinguishableFromEveryOther crosses the vocabulary with itself, so a
// sentinel that started matching another fails the row naming both rather than hiding behind a
// neighbour. A shared "maintenance failed" error would make Steps 8, 10, 11, 13, 14 and 15's
// assertions unwritable.
func TestEverySentinelIsDistinguishableFromEveryOther(t *testing.T) {
	for i, one := range packageSentinels {
		for j, other := range packageSentinels {
			if i == j {
				continue
			}
			if errors.Is(one, other) {
				t.Errorf("errors.Is(%v, %v) is true, and they are meant to be two reasons", one, other)
			}
		}
		if !errors.Is(one, one) {
			t.Errorf("errors.Is(%v, itself) is false", one)
		}
	}
}

// sentinelDetail is one *condition's* carrier, the detail its criterion requires it to name, and the
// step that consumes it. The wording is owned here rather than by the consuming step, so several
// callers cannot render several spellings of one condition.
//
// One row per condition and not per sentinel. ErrUnservableHorizon reports three conditions with
// three remedies, and a registry keyed on the sentinel would let the later ones inherit the first's
// row -- their messages could then be emptied or rewritten with this suite green.
var sentinelDetail = []struct {
	name     string
	built    error
	sentinel error
	names    []string
}{
	{
		name:     "ErrSchemaAhead names both versions (criterion 14, Step 10)",
		built:    schemaAhead(7, 2),
		sentinel: ErrSchemaAhead,
		names:    []string{"7", "2"},
	},
	{
		name:     "ErrCoverageShort names the shortfall and the horizon (criterion 31, Step 13)",
		built:    coverageShort(90*time.Minute, 72*time.Hour),
		sentinel: ErrCoverageShort,
		names:    []string{"1h30m0s", "72h0m0s"},
	},
	{
		name:     "ErrPrivilege names the schema and the missing privilege (criterion 17, Step 13)",
		built:    privilegeMissing("noty", "CREATE"),
		sentinel: ErrPrivilege,
		names:    []string{"noty", "CREATE"},
	},
	{
		name:     "ErrDefaultBlocked names the range it refused (criterion 30, Steps 8 and 11)",
		built:    defaultBlocked("events_20260101t000000000000000z_20260201t000000000000000z"),
		sentinel: ErrDefaultBlocked,
		names:    []string{"events_20260101t000000000000000z_20260201t000000000000000z"},
	},
	{
		name:     "ErrForeignInstance names both instances (Step 13)",
		built:    foreignInstance("orders-eu", "orders-us"),
		sentinel: ErrForeignInstance,
		names:    []string{"orders-eu", "orders-us"},
	},
	{
		name:     "ErrLockTimeout names the operation and the timeout (criterion 40, Steps 8, 14 and 15)",
		built:    lockTimedOut("ATTACH PARTITION", DefaultLockTimeout),
		sentinel: ErrLockTimeout,
		names:    []string{"ATTACH PARTITION", "3s"},
	},
	{
		// The count is the one a one-microsecond grid asks for over a horizon of exactly the
		// identifier space in microseconds: 4,294,967,295 whole intervals, and the k-range is
		// inclusive at both ends -- one more than that.
		name: "ErrUnservableHorizon, over-numerous, names the count, the bound and both durations " +
			"(Steps 11 and 13)",
		built:    unservableHorizon(4_294_967_296, time.Microsecond, 4_294_967_295*time.Microsecond),
		sentinel: ErrUnservableHorizon,
		names:    []string{"4294967296", "1µs", "1h11m34.967295s"},
	},
	{
		// The second condition under the same sentinel, with its own fixture and its own wording. The
		// interval is internal/config's finest, which R10 accepts and a timestamptz cannot keep whole.
		name:     "ErrUnservableHorizon, unstorable interval, names the interval and the granularity",
		built:    unstorableInterval(time.Nanosecond),
		sentinel: ErrUnservableHorizon,
		names:    []string{"1ns", "1µs", "whole multiple", "rounded"},
	},
	{
		// The third, whose remedy is neither of the other two: an interval no rounding and no
		// shorter horizon can rescue, because its sign is what is wrong.
		name:     "ErrUnservableHorizon, negative interval, names the interval and the remedy",
		built:    negativeInterval(-time.Hour),
		sentinel: ErrUnservableHorizon,
		names:    []string{"-1h0m0s", "backwards", "positive duration"},
	},
	{
		// The schema name, because the remedy is a command taking that schema's configuration file
		// and an operator running several instances has to know which one was never bootstrapped.
		name:     "ErrLedgerAbsent names the schema that was never bootstrapped",
		built:    ledgerAbsent("bookings"),
		sentinel: ErrLedgerAbsent,
		names:    []string{"bookings", "records no applied migration"},
	},
}

// TestEachSentinelCarriesTheDetailItsCriterionRequires asserts both halves at once: the carrier
// matches its own sentinel and none of the other six, and its message names the values the
// criterion says it must name.
//
// The reconciliation is per sentinel and the count is per condition, because those are two different
// gaps: a sentinel with no row is a class nothing pins the wording of, and a condition sharing a row
// is one whose wording is pinned by its neighbour's.
func TestEachSentinelCarriesTheDetailItsCriterionRequires(t *testing.T) {
	if len(sentinelDetail) != 10 {
		t.Fatalf("%d carriers are declared for %d sentinels; the vocabulary reports ten conditions "+
			"-- one per sentinel, and two more under ErrUnservableHorizon -- so update this count with "+
			"the condition that joined or left", len(sentinelDetail), len(packageSentinels))
	}
	for _, declared := range packageSentinels {
		if !carriedBySomeRow(declared) {
			t.Errorf("%v carries no row here, so nothing pins what it says", declared)
		}
	}

	for _, tc := range sentinelDetail {
		t.Run(tc.name, func(t *testing.T) {
			assertMatchesOnly(t, tc.built, tc.sentinel)
			for _, named := range tc.names {
				if !strings.Contains(tc.built.Error(), named) {
					t.Errorf("the message %q does not name %q", tc.built, named)
				}
			}
		})
	}
}

// carriedBySomeRow reports whether one sentinel has a carrier above. Identity rather than errors.Is,
// for containsSentinel's reason: the question is whether this very sentinel is the row's subject.
func carriedBySomeRow(want error) bool {
	for _, tc := range sentinelDetail {
		if tc.sentinel == want {
			return true
		}
	}
	return false
}

// assertMatchesOnly asserts an error matches one member of the vocabulary and no other.
func assertMatchesOnly(t *testing.T, err, want error) {
	t.Helper()

	if !errors.Is(err, want) {
		t.Fatalf("errors.Is(%q, %v) is false", err, want)
	}
	for _, other := range packageSentinels {
		if other != want && errors.Is(err, other) {
			t.Errorf("errors.Is(%q, %v) is also true, so a caller cannot tell the two apart", err, other)
		}
	}
}

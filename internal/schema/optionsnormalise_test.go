package schema

import (
	"log/slog"
	"testing"
	"time"
)

// This file holds the zero-value contract of Options; options_test.go holds its shape and its
// constant. Step 4's Expected Output names options_test.go for both, and the two are written across
// two files -- this step's third deviation from it, taken for the reason Step 3 declared the same
// one, that a single file would breach the 200-line budget this package's own gate enforces.

// The three rows below are the zero-value partition, one field at a time. Every caller in Steps 11,
// 13, 14 and 15 may pass a partially-filled Options, so a nil dereference on any of these paths is a
// crash in the maintenance path of a running deployment.

func TestAnUnsetLockTimeoutNormalisesToTheDefault(t *testing.T) {
	if got := (Options{}).normalized().LockTimeout; got != DefaultLockTimeout {
		t.Errorf("a zero LockTimeout normalised to %s, want the default %s", got, DefaultLockTimeout)
	}
}

// TestANilStatsNormalisesToASinkThatCountsRatherThanPanicking is why stats.go carries no nil guard of
// its own: the normaliser is the single place the zero value is made safe, so the counters can assume
// a receiver. The sink is discarded because the caller passed nothing to hold it, which is the whole
// of "discard".
func TestANilStatsNormalisesToASinkThatCountsRatherThanPanicking(t *testing.T) {
	sink := (Options{}).normalized().Stats
	if sink == nil {
		t.Fatal("a nil Stats normalised to nil, so the first counter call is a nil dereference")
	}

	sink.CountFailure()
	sink.CountDefaultPartitionBlocked()
	sink.CountRetentionBlockedByLiveEvents()
	sink.StoreDefaultPartitionRows(7)

	if got := sink.Failures(); got != 1 {
		t.Errorf("the substituted sink read back %d failures, want 1; it is a real counter set that "+
			"nobody holds, not a value that swallows the call", got)
	}
}

func TestANilLoggerNormalisesToTheDefaultLogger(t *testing.T) {
	if got := (Options{}).normalized().Logger; got != slog.Default() {
		t.Errorf("a nil Logger normalised to %p, want slog.Default() at %p", got, slog.Default())
	}
}

// TestNormalisingAnAlreadyNormalisedOptionsChangesNothing is the row a normaliser that assigns the
// defaults unconditionally fails. Without it all three rows above pass while every explicitly
// configured value is silently discarded -- which would make Step 8's and Step 14's lock-timeout
// assertions measure the default rather than the value under test.
func TestNormalisingAnAlreadyNormalisedOptionsChangesNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		start Options
	}{
		{name: "a wholly zero value", start: Options{}},
		{name: "a LockTimeout set explicitly to something other than the default",
			start: Options{LockTimeout: 45 * time.Second}},
		{name: "a wholly filled value", start: Options{LockTimeout: 45 * time.Second,
			Stats: &MaintenanceStats{}, Logger: slog.Default()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			once := tc.start.normalized()

			if twice := once.normalized(); twice != once {
				t.Errorf("normalising twice gave %+v, once gave %+v; the normaliser is not a fixed "+
					"point, so an explicitly configured value cannot survive a second consumer",
					twice, once)
			}
		})
	}
}

// TestAnExplicitLockTimeoutSurvivesNormalisation is the same claim from the side that matters to
// Steps 8, 11 and 14: the field they configure, through two normalisations, unchanged.
func TestAnExplicitLockTimeoutSurvivesNormalisation(t *testing.T) {
	// Deliberately not the default, so an unconditional assignment is visible rather than absorbed.
	const configured = 45 * time.Second

	if got := (Options{LockTimeout: configured}).normalized().normalized().LockTimeout; got != configured {
		t.Errorf("an explicitly configured LockTimeout read back %s after two normalisations, want %s",
			got, configured)
	}
}

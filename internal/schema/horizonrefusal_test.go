package schema

import (
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
)

// This file is the horizon refusal at the two entry points that own an error channel. It is
// container-free for the reason the refusal is about: it fires before a connection is taken and
// before the catalog is read, so a row needing a database would already have disproved the property
// it was written for -- the same reason bootstrap_test.go's criterion 2 rows need none.
//
// horizonbound_test.go holds the bounds and plancardinality_test.go the arithmetic; what is asserted
// here is that both entry points ask, and that neither allocates the set to find out. Which reason
// an input breaking two rules at once is told about is horizonprecedence_test.go's.
//
// Every case is crossed with both conditions rather than written for one. Only one of them
// existed when this file was first written, and a second condition inheriting the first one's
// cases would be reachable from neither entry point with nothing failing.

// unservableConditions is one configuration per condition the guard reports, with the count it asks
// for and the refusal it earns. Both counts are derived from the k-range here and never read back
// from a run.
var unservableConditions = []struct {
	name      string
	retention config.Retention
	// wantCount is what the configuration asks for, which is also the byte floor the allocation
	// measurement below reads.
	wantCount int64
	want      error
}{
	{
		// The recorded obligation: internal/config's finest interval beside the 48h horizon its own
		// defaults block writes. 48h is 172,800 seconds, so a one-nanosecond grid puts the horizon
		// 172,800,000,000,000 intervals away and the k-range is inclusive at both ends.
		name: "an interval finer than the instant a timestamptz stores",
		retention: config.Retention{
			PartitionInterval: time.Nanosecond, Precreate: 48 * time.Hour, Keep: 48 * time.Hour,
		},
		wantCount: 172_800_000_000_001,
		want:      unstorableInterval(time.Nanosecond),
	},
	{
		// The other condition, on the finest interval that clears the first one: 4,294,967,295 whole
		// microseconds divide exactly, so the count is one more -- the identifier space exactly.
		name: "a horizon asking for the whole relation identifier space",
		retention: config.Retention{
			PartitionInterval: time.Microsecond,
			Precreate:         4_294_967_295 * time.Microsecond, Keep: 72 * time.Hour,
		},
		wantCount: 4_294_967_296,
		want:      unservableHorizon(4_294_967_296, time.Microsecond, 4_294_967_295*time.Microsecond),
	},
}

// unservableConfiguration is one such retention on a schema step 1 accepts, so the only rule the
// configuration breaks is the horizon.
func unservableConfiguration(retention config.Retention) config.Config {
	cfg := configWithSchema(harnessSchema)
	cfg.Retention = retention
	return cfg
}

// TestBootstrapRefusesAnUnservableHorizonWithNoConnectionAtAll is the refusal at the boot, asserted
// as strongly as it can be: the pool handed in is nil, so any implementation reaching step 2 first
// dereferences it rather than answering. A refusal that needed a database could not have fired
// before one was used, which is the whole of what this guard is for.
func TestBootstrapRefusesAnUnservableHorizonWithNoConnectionAtAll(t *testing.T) {
	for _, tc := range unservableConditions {
		t.Run(
			tc.name, func(t *testing.T) {
				assertRefusedAs(t, Bootstrap(t.Context(), nil, unservableConfiguration(tc.retention)), tc.want)
			},
		)
	}
}

// TestMaintainRefusesAnUnservableHorizonBeforeItReadsAnything is the same refusal at the other entry
// point, with criterion 33's two observability clauses: the failure counter a caller reads with no
// metrics endpoint, and a line at a level an alerting rule can key on. The handle is nil for the
// same reason the pool above is.
//
// The pass is the one that would allocate, so a boot is not enough to close this: internal/cli calls
// Maintain on its own timer, and Bootstrap's refusal says nothing about that path.
func TestMaintainRefusesAnUnservableHorizonBeforeItReadsAnything(t *testing.T) {
	for _, tc := range unservableConditions {
		t.Run(
			tc.name, func(t *testing.T) {
				stats, recorder := &MaintenanceStats{}, &logRecorder{}

				result, refused := Maintain(
					t.Context(), nil, unservableConfiguration(tc.retention),
					Options{Stats: stats, Logger: slog.New(recorder)},
				)

				assertRefusedAs(t, refused, tc.want)
				if result.Outcome != OutcomeFailed {
					t.Errorf(
						"the pass reported outcome %q, want %q; a refused configuration must not "+
							"read as a pass with nothing to do", result.Outcome, OutcomeFailed,
					)
				}
				if stats.Failures() != 1 {
					t.Errorf(
						"the failure counter reads %d, want 1; criterion 33 is what a caller with "+
							"no metrics endpoint has", stats.Failures(),
					)
				}
				assertOneUnservableLine(t, recorder.taken())
			},
		)
	}
}

// assertOneUnservableLine checks the line's clauses one at a time, so a record missing one fails the
// clause named for it. Its message is asserted by equality against the constant, because a refused
// configuration and an unreadable catalog are two conditions with two remedies and only the wording
// keeps them apart.
func assertOneUnservableLine(t *testing.T, written []loggedRecord) {
	t.Helper()

	if unservableMessage == unobservedMessage {
		t.Fatalf(
			"a refused configuration and an unreadable catalog both report %q; they have two "+
				"remedies -- change the configuration, or wait for the next tick -- and the wording is "+
				"the only thing keeping them apart", unservableMessage,
		)
	}
	if len(written) != 1 {
		t.Fatalf("the pass wrote %d records %v, want the one that says it refused", len(written), written)
	}
	if written[0].message != unservableMessage {
		t.Errorf("the line reads %q, want %q", written[0].message, unservableMessage)
	}
	if written[0].level != slog.LevelError {
		t.Errorf(
			"the line is at level %v, want %v; a failing loop that reads like routine "+
				"operation is one no alerting rule can find", written[0].level, slog.LevelError,
		)
	}
	if cause := written[0].attrs[logCause]; !strings.Contains(cause, ErrUnservableHorizon.Error()) {
		t.Errorf("the line's %s attribute is %q and does not carry the refusal", logCause, cause)
	}
}

// TestTheBootRefusesAnUnservableHorizonWithoutBuildingIt is the measurement that makes "refused
// before it is allocated" a fact rather than a claim. The bound is derived and not chosen: a refusal
// that had to build the set would allocate at least one byte per Range, and a Range is two time.Time
// and a string -- so the count itself, taken as bytes, is a floor no materialising implementation
// can come in under. The figure actually measured is logged rather than written into this comment,
// where it could only rot.
func TestTheBootRefusesAnUnservableHorizonWithoutBuildingIt(t *testing.T) {
	for _, tc := range unservableConditions {
		t.Run(
			tc.name, func(t *testing.T) {
				var before, after runtime.MemStats

				runtime.GC()
				runtime.ReadMemStats(&before)
				refused := Bootstrap(t.Context(), nil, unservableConfiguration(tc.retention))
				runtime.ReadMemStats(&after)

				if !errors.Is(refused, ErrUnservableHorizon) {
					t.Fatalf("the boot answered %v, so this measurement is of the wrong path", refused)
				}
				allocated := after.TotalAlloc - before.TotalAlloc
				t.Logf("refusing %d partitions allocated %d bytes", tc.wantCount, allocated)
				if allocated >= uint64(tc.wantCount) {
					t.Errorf(
						"refusing allocated %d bytes, which is at or above the %d-byte floor of one "+
							"byte per Range asked for; the set is being built before it is refused",
						allocated, tc.wantCount,
					)
				}
			},
		)
	}
}

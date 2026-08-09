//go:build integration

package reconcile

import (
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is the readback half of the credential rules. Its oracle is
// credentialmarker_integration_test.go.

// credentialRun is one of the five rendering runs. Each sets up its own database state and returns
// what it rendered; wantStopped is what the run is named for, asserted so that a run which stopped
// somewhere else cannot stand in for the surface it was meant to produce.
type credentialRun struct {
	name        string
	wantStopped StopReason
	drive       func(t *testing.T, pool *pgxpool.Pool) (runOutcome, StopReason)
}

// TestNoCredentialMarkerReachesAnyRenderedSurface is every rendered surface across all five runs plus
// the database readback. Every marker is searched for on every surface of every run, not
// only on the first.
func TestNoCredentialMarkerReachesAnyRenderedSurface(t *testing.T) {
	skipIfShort(t)

	runs := credentialRuns()
	if len(runs) != 5 {
		t.Fatalf("the rendering surfaces are enumerated over five runs and this drives %d", len(runs))
	}
	for _, run := range runs {
		t.Run(
			run.name, func(t *testing.T) {
				pool := freshDatabase(t)
				outcome, stopped := run.drive(t, pool)

				if stopped != run.wantStopped {
					t.Fatalf(
						"run stopped at %q, want %q; this run no longer produces the surface it is "+
							"named for", stopped, run.wantStopped,
					)
				}
				assertNoMarkerReaches(t, run.name, outcome.surfaces(), plantedMarkerList(t))
			},
		)
	}
}

// plantedMarkerList is the watch list every run is searched against, built by the same planting the
// runs use so a field added to plantCredentials extends the oracle by existing.
func plantedMarkerList(t *testing.T) []string {
	t.Helper()
	return plantCredentials(t, "credential_markers").markers
}

func credentialRuns() []credentialRun {
	return []credentialRun{
		{
			name: "planned", drive: func(t *testing.T, pool *pgxpool.Pool) (runOutcome, StopReason) {
				planted := preparedCredentialTarget(t, pool, "credential_planned")
				return recordedRun(t, pool, planted.cfg, Options{}, false), ""
			},
		},
		{
			name: "applied", drive: func(t *testing.T, pool *pgxpool.Pool) (runOutcome, StopReason) {
				planted := preparedCredentialTarget(t, pool, "credential_applied")
				outcome := recordedRun(t, pool, planted.cfg, Options{}, true)
				assertNoMarkerReachesTheDatabase(t, pool, planted.markers)
				return outcome, ""
			},
		},
		{name: "failed on validation", wantStopped: StopValidation, drive: credentialValidationRun},
		{name: "failed on ownership", wantStopped: StopOwnership, drive: credentialOwnershipRun},
		{name: "failed on a lock timeout", drive: credentialLockTimeoutRun},
	}
}

// preparedCredentialTarget creates the bootstrap objects and the target table, and returns the
// marked configuration naming it.
func preparedCredentialTarget(t *testing.T, pool *pgxpool.Pool, table string) plantedCredentials {
	t.Helper()
	prepareOwnershipDatabase(t, pool)
	mustExecOn(t, pool, "CREATE TABLE "+mustQualifiedTarget(t, table)+" (id bigint PRIMARY KEY, state text)")
	return plantCredentials(t, table)
}

// credentialValidationRun induces a diagnostic with an unresolvable WHEN column, which is the surface
// a rendered diagnostic is most likely to quote configuration into.
func credentialValidationRun(t *testing.T, pool *pgxpool.Pool) (runOutcome, StopReason) {
	planted := preparedCredentialTarget(t, pool, "credential_validation")
	planted.cfg.Listeners[0].Trigger.Operations[0].When = "NEW.missing > 0"

	outcome := recordedRun(t, pool, planted.cfg, Options{}, true)
	if len(outcome.plan.Diagnostics) == 0 {
		t.Fatal("the validation run produced no diagnostic, so its diagnostic surface is empty")
	}
	return outcome, StopValidation
}

// credentialOwnershipRun applies the marked configuration, strips the ownership comment off the
// resulting trigger and then removes the listener, so the drop meets an object it cannot prove it
// owns.
func credentialOwnershipRun(t *testing.T, pool *pgxpool.Pool) (runOutcome, StopReason) {
	planted := preparedCredentialTarget(t, pool, "credential_ownership")
	if _, err := Apply(t.Context(), pool, planted.cfg, Approval{Approved: true}, Options{}); err != nil {
		t.Fatal(err)
	}
	reading, err := readRegistry(t.Context(), pool, harnessSchema, planted.listener.Name)
	if err != nil || len(reading.Triggers) != 1 {
		t.Fatalf("registry = %+v, %v", reading, err)
	}
	trigger, fault := schema.Quoted(reading.Triggers[0].TriggerName)
	if fault != schema.IdentifierOK {
		t.Fatal(fault)
	}
	target := mustQualifiedTarget(t, "credential_ownership")
	mustExecOn(t, pool, "COMMENT ON TRIGGER "+trigger+" ON "+target+" IS NULL")

	stripped := planted.cfg
	stripped.Listeners = []config.Listener{}
	outcome := recordedRun(t, pool, stripped, Options{}, true)
	if len(outcome.plan.Refusals) == 0 {
		t.Fatal("the ownership run produced no refusal, so its refusal surface is empty")
	}
	return outcome, StopOwnership
}

// credentialLockTimeoutRun holds ACCESS EXCLUSIVE on the target from another session so the apply's
// own table lock expires, which is the run whose error text names the table and the blocker.
func credentialLockTimeoutRun(t *testing.T, pool *pgxpool.Pool) (runOutcome, StopReason) {
	planted := preparedCredentialTarget(t, pool, "credential_lock_timeout")
	holder, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	mustExecOn(t, holder, "BEGIN")
	defer mustExecOn(t, holder, "ROLLBACK")
	mustExecOn(t, holder, "LOCK TABLE "+mustQualifiedTarget(t, "credential_lock_timeout")+" IN ACCESS EXCLUSIVE MODE")

	outcome := recordedRun(t, pool, planted.cfg, Options{LockTimeout: 50 * time.Millisecond}, true)
	if outcome.err == nil {
		t.Fatal("the lock-timeout run returned no error, so its error surface is empty")
	}
	return outcome, ""
}

// assertNoMarkerReachesTheDatabase is the database half: no marker reaches noty.listeners,
// noty.listener_triggers or any value this package wrote. Each row is read back whole as text, so a
// column added later is covered without this assertion being revisited, and the readback is asserted
// non-empty because a query returning nothing satisfies the search vacuously.
func assertNoMarkerReachesTheDatabase(t *testing.T, pool *pgxpool.Pool, markers []string) {
	t.Helper()

	written := map[string]string{}
	for _, table := range []string{schema.TableListeners, schema.TableListenerTriggers} {
		qualified, err := qualifiedRegistryTable(harnessSchema, table)
		if err != nil {
			t.Fatal(err)
		}
		var rows string
		query := "SELECT coalesce(string_agg(entry::text, ' '), '') FROM " + qualified + " AS entry"
		if err := pool.QueryRow(t.Context(), query).Scan(&rows); err != nil {
			t.Fatalf("read back %s: %v", qualified, err)
		}
		if strings.TrimSpace(rows) == "" {
			t.Fatalf("%s read back empty, so searching it proves nothing about what was written", qualified)
		}
		written[qualified] = rows
	}
	assertNoMarkerReaches(t, "database readback", written, markers)
}

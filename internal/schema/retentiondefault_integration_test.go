//go:build integration

package schema

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 36 and criterion 29's retention half: the DEFAULT partition is never
// dropped, never detached and never re-created by a retention pass. Step 11 asserts the maintenance
// half over a converged schema and no row of it is cited here -- the two halves are separate
// fixtures and neither is implied by the other (D3), so this file builds its own and reaches the
// condition Step 11 never can, which is a schema where *every* range partition is expired.
//
// Existence is not the assertion. A DEFAULT partition dropped and re-created under its own name
// still exists, and a pass that did that would have made every unrouted write fail in the window
// between -- so the identity is compared across the pass and the attachment is read back.

// theDefaultPartitionState is what a retention pass may not change about the safety net: which
// object it is, and that it is still the parent's DEFAULT partition.
type theDefaultPartitionState struct {
	identity int64
	attached bool
	tagged   string
}

// defaultPartitionState reads all three through the observation authority, so this case and the pass
// cannot disagree about what the catalog says.
func defaultPartitionState(t *testing.T, pool *pgxpool.Pool) theDefaultPartitionState {
	t.Helper()

	identity, rendered := identityOf(t, pool, harnessSchema, PartitionDefault)
	if rendered == "" {
		t.Fatalf("%s.%s does not exist, so comparing its state across a pass would compare two "+
			"absences", harnessSchema, PartitionDefault)
	}
	attached, err := isPartitionOf(t.Context(), pool, harnessSchema, PartitionDefault, TableEvents)
	if err != nil {
		t.Fatalf("ask whether %s is attached: %v", PartitionDefault, err)
	}
	found, err := observePartitions(t.Context(), pool, harnessSchema, TableEvents)
	if err != nil {
		t.Fatalf("observe the partitions of %s: %v", TableEvents, err)
	}
	return theDefaultPartitionState{identity: identity, attached: attached, tagged: found.Default}
}

// TestTheDefaultPartitionOutlivesEveryRetentionPassUnchanged is criteria 36 and 29's retention half.
// The all-expired row is the decisive one: a predicate treating an unbounded partition as infinitely
// old passes every other input in this package and fails only here.
func TestTheDefaultPartitionOutlivesEveryRetentionPassUnchanged(t *testing.T) {
	skipIfShort(t)

	for _, tc := range []struct {
		name string
		// expired is how many wholly-expired range partitions the schema holds; kept is how many
		// still hold an instant inside the window.
		expired, kept int
	}{
		{name: "a schema holding expired ranges and current ones", expired: 2, kept: 2},
		{name: "a schema whose every range partition is expired", expired: 3},
		{name: "a schema holding no expired range at all", kept: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool, cfg := aRetainedEventLog(t)
			interval := cfg.Retention.PartitionInterval
			cutoff := theClockOf(t, pool).Add(-cfg.Retention.Keep)

			plantMarkedPartitions(t, pool, rangesEndingBefore(cutoff, interval, tc.expired))
			plantMarkedPartitions(t, pool,
				rangesEndingBefore(cutoff.Add(cfg.Retention.Keep), interval, tc.kept))
			before := defaultPartitionState(t, pool)

			report := oneRetentionPass(t, pool, cfg, Options{})

			if want := (Result{Outcome: outcomeOf(tc.expired, 0), Dropped: tc.expired}); report.settled !=
				want || report.refused != nil {
				t.Errorf("the pass reported %+v and %v, want %+v and no error",
					report.settled, report.refused, want)
			}
			if after := defaultPartitionState(t, pool); after != before {
				t.Errorf("%s reads as %+v after the pass and read as %+v before it; it is created by "+
					"migration 2 and a maintenance pass neither drops, detaches nor re-creates it",
					PartitionDefault, after, before)
			}
		})
	}
}

// TestAPartitionOfOurParentInAnotherSchemaStopsThePassRatherThanBeingDropped is the third axis the
// two decoys in retentionblastradius_integration_test.go leave open: same parent, different schema.
// It cannot be planted alongside them, because catalog.go refuses an observation that found one --
// this package may neither drop a table outside the configured schema nor decide without it -- so
// the property here is the fail-closed answer rather than survival through a completed pass.
func TestAPartitionOfOurParentInAnotherSchemaStopsThePassRatherThanBeingDropped(t *testing.T) {
	skipIfShort(t)

	pool, cfg := aRetainedEventLog(t)
	interval := cfg.Retention.PartitionInterval
	cutoff := theClockOf(t, pool).Add(-cfg.Retention.Keep)
	named := rangesEndingBefore(cutoff, interval, 2)

	plantMarkedPartitions(t, pool, named[:1])
	mustExecOn(t, pool, "CREATE SCHEMA "+mustQuote(t, otherSchema))
	mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, otherSchema, named[1].Name)+
		" PARTITION OF "+mustQualify(t, harnessSchema, TableEvents)+forValues(named[1]))
	claimPartition(t, pool, otherSchema, named[1].Name)
	assertOwnedByThisInstance(t, pool, otherSchema, named[1].Name)

	report := oneRetentionPass(t, pool, cfg, Options{})

	if want := (Result{Outcome: OutcomeFailed}); report.settled != want || report.refused == nil {
		t.Fatalf("the pass reported %+v and %v, want %+v and a refusal: a partition of this parent "+
			"outside the configured schema is one this package may neither drop nor decide without",
			report.settled, report.refused, want)
	}
	for _, schemaName := range []string{otherSchema, harnessSchema} {
		if !strings.Contains(report.refused.Error(), schemaName) {
			t.Errorf("the refusal %q does not name schema %s", report.refused, schemaName)
		}
	}
	survives(t, pool, otherSchema, named[1].Name,
		"a partition living outside the configured schema is outside what this pass may drop")
	survives(t, pool, harnessSchema, named[0].Name,
		"an observation this pass could not complete decides nothing, so it drops nothing")
	if want := (statsReading{failures: 1}); report.stats != want {
		t.Errorf("the pass recorded %+v, want %+v", report.stats, want)
	}
}

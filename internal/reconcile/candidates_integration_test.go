//go:build integration

package reconcile

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// TestEveryCandidateOnAHealthyInstallProvesOwned is the assertion destroy actually depends on. The
// previous form of this test asserted only that OwnedCandidates returned a non-nil slice without an
// error, against a database with no target table and no applied trigger -- so it passed while every
// function candidate carried a zero CatalogObject. The recorded pass read function ownership with
// the *trigger's* OID, which pg_proc never matches, and deduplicateCandidates keeps the first
// occurrence, so the empty candidate displaced the correct one from the configured pass. destroy
// refused every healthy install with a nameless " is not proven owned ()".
//
// The function population is counted before it is judged, so a run that stops producing function
// candidates fails by name rather than passing vacuously.
func TestEveryCandidateOnAHealthyInstallProvesOwned(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareOwnershipDatabase(t, pool)
	target := mustQualifiedTarget(t, "candidate_ownership_target")
	mustExecOn(t, pool, "CREATE TABLE "+target+" (id bigint PRIMARY KEY)")
	listener := listenerForTarget("public.candidate_ownership_target")
	listener.Enabled = true
	cfg := harnessConfig(t)
	cfg.Listeners = []config.Listener{listener}
	if applied, err := Apply(t.Context(), pool, cfg, Approval{Approved: true}, Options{}); err != nil ||
		applied.Verdict != VerdictClean || applied.Statements == 0 {
		t.Fatalf("apply = %+v, %v; want a changed clean result", applied, err)
	}

	candidates, err := OwnedCandidates(t.Context(), pool, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}

	functions := 0
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate.identity, "function:") {
			functions++
		}
		ownership := DetermineOwnership(candidate.Registry, candidate.Catalog, cfg.Instance)
		if !ownership.Owned {
			t.Errorf(
				"candidate %q is not owned (disagreement %q, found %q); destroy refuses the whole "+
					"run on the first unowned candidate, so a healthy install could never be destroyed",
				candidate.identity, ownership.Disagreement, ownership.Found,
			)
		}
	}
	if functions == 0 {
		t.Fatal(
			"no function candidate was produced, so the ownership assertion above covers only " +
				"triggers and the function half of destroy's proof is untested",
		)
	}
}

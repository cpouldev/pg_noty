//go:build integration

package schema

import "testing"

func TestAppliedVersionReadsTheEmbeddedLedgerAndRejectsAbsence(t *testing.T) {
	skipIfShort(t)
	cfg := harnessConfig(t)
	pool := emptySchemas(t, harnessSchema)
	corpus := embeddedCorpusOrFail(t)
	if _, err := AppliedVersion(t.Context(), pool, cfg, Options{}); err == nil {
		t.Fatal("an absent schema ledger returned version zero and nil")
	}
	mustMigrate(t, pool, harnessSchema, corpus)
	want := highestEmbedded(corpus)
	got, err := AppliedVersion(t.Context(), pool, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("AppliedVersion = %d, want migration-set maximum %d", got, want)
	}
}

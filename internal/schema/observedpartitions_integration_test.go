//go:build integration

package schema

import (
	"testing"
)

func TestObservedPartitionsFeedsCoverageShortfall(t *testing.T) {
	skipIfShort(t)
	pool := eventLogFixture(t)
	cfg := harnessConfig(t)
	cfg.Retention = theObservedRetention
	want := RequiredRanges(theObservedInstant, cfg.Retention)
	plantPartitions(t, pool, harnessSchema, want)
	got, err := ObservedPartitions(t.Context(), pool, cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("ObservedPartitions = %+v, want arithmetic ranges %+v", got, want)
	}
	for index := range want {
		if got[index].Name != want[index].Name || !got[index].From.Equal(want[index].From) ||
			!got[index].To.Equal(want[index].To) {
			t.Fatalf("ObservedPartitions[%d] = %+v, want arithmetic range %+v", index, got[index], want[index])
		}
	}
	if shortfall := CoverageShortfall(got[:len(got)-1], theObservedInstant, cfg.Retention.Precreate); shortfall <= 0 {
		t.Fatal("a missing final range did not produce a positive coverage shortfall")
	}
	if shortfall := CoverageShortfall(got, theObservedInstant, cfg.Retention.Precreate); shortfall != 0 {
		t.Fatalf("complete coverage shortfall = %s, want zero", shortfall)
	}
}

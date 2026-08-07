package config

import (
	"testing"
	"time"
)

func TestGreekMuDurationUnitIsRejectedAtTheParserBoundary(t *testing.T) {
	const greekMu = "1μs" // U+03BC, distinct from the ratified micro sign U+00B5.
	if _, err := time.ParseDuration(greekMu); err != nil {
		t.Fatalf("the standard-library control unexpectedly rejects %q: %v", greekMu, err)
	}
	if _, ok := asDuration(greekMu); ok {
		t.Errorf("asDuration accepts Greek mu in %q; only ASCII us and micro sign µs are ratified", greekMu)
	}
}

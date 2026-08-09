package schema

import (
	"strings"
	"testing"
)

// This file is the refusing half of the inventory, split from cataloginventory_test.go the way
// partitionboundrefusal_test.go is split from the reader it refuses for: that every reason add
// declines a row is reached by a real input, and that the skip a dropped partition earns was not
// quietly widened into tolerating anything the observation could not read.

// aSchemaOutsideTheConfiguredOne is where a child out of this package's remit is planted here. It is
// this tier's own constant rather than catalog_integration_test.go's otherSchema, which the
// integration build tag puts out of reach of a container-free case.
const aSchemaOutsideTheConfiguredOne = "elsewhere"

// TestAChildDeclaringNoBoundRefusesTheWholeObservation is the refusal the skip must not have
// swallowed. A row that declares no bound is not a partition at all, so the parent under observation
// is not partitioned -- and that is refused rather than tolerated as one more falsy bound.
func TestAChildDeclaringNoBoundRefusesTheWholeObservation(t *testing.T) {
	var found observedPartitions

	err := found.add(harnessSchema, "classic_child", harnessSchema, boundReadFrom(false, nil))
	if err == nil {
		t.Fatal("a child declaring no partition bound was accepted; an observation that cannot tell " +
			"a partition from an inherited table is one every drop decision is taken against")
	}
	for _, want := range []string{"classic_child", "declares no partition bound", "not partitioned"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal reads %q, which does not name %q", err, want)
		}
	}
}

// TestABoundReadingNoObservationCanProduceIsRefusedRatherThanSkipped reaches the fail-closed default
// by constructing its state directly, so deleting the refusal fails a test instead of leaving the
// arm silently falling through to a skip. TestEveryPairOfBoundColumnsResolvesToADeclaredReading is
// what proves nothing a catalog row holds gets here, which is what makes refusing it unconditionally
// affordable.
func TestABoundReadingNoObservationCanProduceIsRefusedRatherThanSkipped(t *testing.T) {
	var found observedPartitions

	err := found.add(harnessSchema, "events_january", harnessSchema, observedBound{})
	if err == nil {
		t.Fatal("a reading none of the three declared ones was skipped rather than refused")
	}
	for _, want := range []string{"events_january", "does not recognise"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal reads %q, which does not name %q", err, want)
		}
	}
	if len(found.Bounded) != 0 {
		t.Errorf("the inventory took %v from a reading it does not recognise", found.Bounded)
	}
}

// TestAVanishedChildOfAnotherSchemaIsStillRefusedForBeingOutOfRemit pins the order add documents.
// The row violates both rules at once, so only a schema-before-reading order answers the schema --
// and a reader that skipped it as somebody else's race would report a foreign schema's partition as
// nothing at all.
func TestAVanishedChildOfAnotherSchemaIsStillRefusedForBeingOutOfRemit(t *testing.T) {
	var found observedPartitions

	err := found.add(harnessSchema, "events_january", aSchemaOutsideTheConfiguredOne,
		boundReadFrom(true, nil))
	if err == nil {
		t.Fatalf("a vanished partition living in %s was skipped; a child outside the configured "+
			"schema is out of remit whatever its bound says", aSchemaOutsideTheConfiguredOne)
	}
	if !strings.Contains(err.Error(), aSchemaOutsideTheConfiguredOne) {
		t.Errorf("the refusal reads %q, which does not name the schema %q it refused for",
			err, aSchemaOutsideTheConfiguredOne)
	}
}

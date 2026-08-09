package schema

import "testing"

// TestEveryRecordedBoundIsReadOrRefusedForItsOwnReason is the reader's whole surface, one row per
// class. A refused row asserts the reason and not merely the refusal, so a rewrite that refuses the
// right inputs for the wrong reason fails here.
func TestEveryRecordedBoundIsReadOrRefusedForItsOwnReason(t *testing.T) {
	for _, tc := range recordedBounds {
		t.Run(tc.name, func(t *testing.T) {
			extent, fault := rangeFrom(tc.written, "observed_partition")

			if fault != tc.fault {
				t.Fatalf("rangeFrom(%s) answered %q, want %q", tc.written, fault, tc.fault)
			}
			if fault != boundOK {
				return
			}
			if !extent.From.Equal(tc.from) || !extent.To.Equal(tc.to) {
				t.Errorf("rangeFrom(%s) read [%s, %s), want [%s, %s)",
					tc.written, extent.From, extent.To, tc.from, tc.to)
			}
			if extent.Name != "observed_partition" {
				t.Errorf("the extent is named %s, want the name the catalog reported", extent.Name)
			}
		})
	}
}

// TestOnlyTheDefaultExpressionIsReadAsTheDefaultPartition is the other half of the same rows, and
// it is what keeps the DEFAULT partition out of every Range: isDefaultBound accepts one string, and
// rangeFrom refuses that same string rather than reading it as a bound with a zero end. A DEFAULT
// partition given a comparable upper bound is what would make retention read it as older than every
// cutoff and drop the safety net every unrouted row lands in (criterion 36).
func TestOnlyTheDefaultExpressionIsReadAsTheDefaultPartition(t *testing.T) {
	defaults := 0
	for _, tc := range recordedBounds {
		if got := isDefaultBound(tc.written); got != tc.isDefault {
			t.Errorf("isDefaultBound(%s) = %t, want %t", tc.written, got, tc.isDefault)
		}
		if tc.isDefault {
			defaults++
		}
	}

	if defaults != 1 {
		t.Fatalf("%d rows claim to be the DEFAULT partition, want exactly 1; without one this "+
			"reconciliation passes over a class nothing exercises", defaults)
	}
}

// TestTheOnlyRowsNoPartitionCanRenderAreTheTwoThatSaySo counts the population the container-backed
// reconciliation ranges over. Without it, a corpus that quietly lost its boundClause fields would
// leave that reconciliation asserting nothing while still passing.
func TestTheOnlyRowsNoPartitionCanRenderAreTheTwoThatSaySo(t *testing.T) {
	var unrenderable []string
	for _, tc := range recordedBounds {
		if tc.boundClause == "" {
			unrenderable = append(unrenderable, tc.name)
		}
	}

	if len(unrenderable) != 2 {
		t.Errorf("%d rows carry no boundClause %v, want the 2 written as inputs no server "+
			"produces; every other row is reconciled against the running server",
			len(unrenderable), unrenderable)
	}
}

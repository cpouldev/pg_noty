package schema

import (
	"reflect"
	"sync"
	"testing"
)

// statsReading is every observation MaintenanceStats carries, read at one instant. The rows below
// assert against a whole reading rather than against the one counter they touched, so "incrementing
// it alone leaves the others unchanged" is the assertion rather than a second, weaker claim beside it.
type statsReading struct {
	failures                     int64
	defaultPartitionBlocked      int64
	defaultPartitionRows         int64
	retentionBlockedByLiveEvents int64
}

// readStats takes one reading through the exported readers -- the same readers criterion 33 requires
// a caller to be able to use with no metrics endpoint, so the corpus exercises the surface the
// criterion names rather than the fields behind it.
func readStats(stats *MaintenanceStats) statsReading {
	return statsReading{
		failures:                     stats.Failures(),
		defaultPartitionBlocked:      stats.DefaultPartitionBlocked(),
		defaultPartitionRows:         stats.DefaultPartitionRows(),
		retentionBlockedByLiveEvents: stats.RetentionBlockedByLiveEvents(),
	}
}

// theObservations is one row per observation MaintenanceStats carries: what records it, and the whole
// reading that must follow from a fresh value. ADR-8's DefaultPartitionBlocked and
// DefaultPartitionRows and ADR-9's RetentionBlockedByLiveEvents each get a row, because a blocked
// range and a stalled retention are otherwise indistinguishable from "nothing to do".
var theObservations = []struct {
	name   string
	record func(*MaintenanceStats)
	want   statsReading
}{
	{
		name:   "the general failure counter",
		record: (*MaintenanceStats).CountFailure,
		want:   statsReading{failures: 1},
	},
	{
		name:   "ADR-8's blocked-range event counter",
		record: (*MaintenanceStats).CountDefaultPartitionBlocked,
		want:   statsReading{defaultPartitionBlocked: 1},
	},
	{
		name:   "ADR-9's stalled-retention counter",
		record: (*MaintenanceStats).CountRetentionBlockedByLiveEvents,
		want:   statsReading{retentionBlockedByLiveEvents: 1},
	},
	{
		name:   "ADR-8's standing-condition gauge",
		record: func(stats *MaintenanceStats) { stats.StoreDefaultPartitionRows(1) },
		want:   statsReading{defaultPartitionRows: 1},
	},
}

// TestEachObservationIsItsOwnAndMovesNoOther is the distinctness claim, one row per observation. A
// counter folded into the general failure count fails the row named for it rather than hiding behind
// a neighbour (.claude/rules/isolate-each-clause-of-a-multi-clause-guard.md).
func TestEachObservationIsItsOwnAndMovesNoOther(t *testing.T) {
	if declared := reflect.TypeOf(statsReading{}).NumField(); len(theObservations) != declared {
		t.Fatalf("%d observations are declared for a reading of %d; add the row with the counter",
			len(theObservations), declared)
	}

	for _, tc := range theObservations {
		t.Run(tc.name, func(t *testing.T) {
			stats := &MaintenanceStats{}

			tc.record(stats)

			if got := readStats(stats); got != tc.want {
				t.Errorf("recording %s read back %+v, want %+v", tc.name, got, tc.want)
			}
		})
	}
}

// TestDefaultPartitionRowsIsAGaugeAndNotACounter is the row a monotonic Add implementation fails.
// Storing 5 then 3 reads back 3: the gauge is the number of rows the DEFAULT partition holds now,
// re-stored each pass, and a sum across passes is a number nothing can threshold, so an alert on it
// would fire and never clear (ADR-8).
func TestDefaultPartitionRowsIsAGaugeAndNotACounter(t *testing.T) {
	stats := &MaintenanceStats{}

	stats.StoreDefaultPartitionRows(5)
	stats.StoreDefaultPartitionRows(3)

	if got := stats.DefaultPartitionRows(); got != 3 {
		t.Errorf("storing 5 then 3 read back %d, want 3; 8 is the accumulating implementation and 5 "+
			"is one that refuses to move once set", got)
	}
	if got := stats.DefaultPartitionBlocked(); got != 0 {
		t.Errorf("storing the gauge moved DefaultPartitionBlocked to %d; the standing condition and "+
			"the refusal event are separate observations", got)
	}
}

// TestConcurrentRecordingLosesNoUpdate runs every observation from many goroutines at once. Plain int
// counters pass every single-goroutine test above and fail only under the three-replica cases in
// Steps 11 and 14, where a lost update reads as a flake -- so the synchronisation is measured here,
// before any concurrent caller exists.
func TestConcurrentRecordingLosesNoUpdate(t *testing.T) {
	const writers, perWriter = 8, 500

	stats := &MaintenanceStats{}
	var running sync.WaitGroup

	for range writers {
		running.Add(1)
		go func() {
			defer running.Done()
			for range perWriter {
				stats.CountFailure()
				stats.CountDefaultPartitionBlocked()
				stats.CountRetentionBlockedByLiveEvents()
				stats.StoreDefaultPartitionRows(3)
			}
		}()
	}
	running.Wait()

	// 8 writers x 500 iterations = 4000 increments of each counter; the gauge is stored, so its
	// answer is the value stored and not the number of stores.
	want := statsReading{failures: writers * perWriter, defaultPartitionBlocked: writers * perWriter,
		defaultPartitionRows: 3, retentionBlockedByLiveEvents: writers * perWriter}

	if got := readStats(stats); got != want {
		t.Errorf("after %d concurrent recordings the stats read %+v, want %+v",
			writers*perWriter, got, want)
	}
}

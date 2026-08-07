package schema

import "sync/atomic"

// Outcome is what one maintenance pass amounted to, in criterion 33's own vocabulary. It exists so
// that a pass which did nothing because nothing was needed is distinguishable from one which did
// nothing because it failed; without the distinction both read as "no work" and a stalled deployment
// looks exactly like a healthy one.
//
// The zero value is deliberately none of the three. A path that forgot to set an outcome must not be
// read as a deliberate report of the one outcome a caller acts on by doing nothing at all.
type Outcome string

const (
	// OutcomeNothingNeeded is a pass that found the partition set already correct. Criterion 22's
	// idempotency is this outcome twice in a row.
	OutcomeNothingNeeded Outcome = "nothing_needed"
	// OutcomeWorkDone is a pass that created or dropped at least one partition, and completed.
	OutcomeWorkDone Outcome = "work_done"
	// OutcomeFailed is a pass that attempted work and could not finish it -- no privilege, a lock it
	// could not take within the timeout, or the DEFAULT-partition refusal.
	OutcomeFailed Outcome = "failed"
)

// Outcomes is the declared set, as a value, so a test can quantify over it and a fourth member has
// to join it.
var Outcomes = []Outcome{OutcomeNothingNeeded, OutcomeWorkDone, OutcomeFailed}

// Result is what one maintenance pass did. Two results are equal exactly when the two passes are
// indistinguishable to a caller, which is what criterion 33 asks for.
type Result struct {
	Outcome Outcome
	// Created and Dropped are how many partitions the pass created and dropped. They are what makes
	// criterion 22's claim observable at the call site -- a second pass over a converged schema
	// reports zero of each -- and criterion 23's, where an expired partition is dropped exactly once
	// across every replica.
	Created int
	Dropped int
}

// MaintenanceStats is the in-process observation set criterion 33 requires a caller to be able to
// read with no metrics endpoint. It carries the general failure counter plus the three observations
// ADR-8 and ADR-9 add, each its own value: a blocked range and a stalled retention folded into the
// general count would be indistinguishable from "nothing to do", which is the state they exist to
// tell apart.
//
// The zero value is ready to use. It is never copied after first use, because an atomic is not
// copyable, and every consumer reaches it through Options.Stats, which is a pointer.
//
// There is no nil receiver guard here on purpose: Options.normalized is the single place a nil Stats
// is made safe, so a guard here would be a second, untested answer to a question already settled.
type MaintenanceStats struct {
	failures                     atomic.Int64
	defaultPartitionBlocked      atomic.Int64
	defaultPartitionRows         atomic.Int64
	retentionBlockedByLiveEvents atomic.Int64
}

// CountFailure records one maintenance pass that could not complete.
func (stats *MaintenanceStats) CountFailure() { stats.failures.Add(1) }

// Failures is how many passes have failed since this value was created.
func (stats *MaintenanceStats) Failures() int64 { return stats.failures.Load() }

// CountDefaultPartitionBlocked records one range whose creation the DEFAULT partition refused
// because rows for it are already sitting there (ADR-8). It is deliberately not the general failure
// counter: this condition is permanent until an operator runs RepairDefaultPartition, so it needs a
// signal of its own rather than a share of a number that also moves for transient causes.
func (stats *MaintenanceStats) CountDefaultPartitionBlocked() {
	stats.defaultPartitionBlocked.Add(1)
}

// DefaultPartitionBlocked is how many range creations the DEFAULT partition has refused.
func (stats *MaintenanceStats) DefaultPartitionBlocked() int64 {
	return stats.defaultPartitionBlocked.Load()
}

// StoreDefaultPartitionRows records how many rows the DEFAULT partition holds right now.
//
// This is the one gauge in the set, and the distinction is load-bearing. It is a standing condition,
// not an event: rows sit in DEFAULT whether or not a create was attempted this pass, and the number
// is re-stored every pass rather than accumulated. Summed across passes it would be a quantity
// nothing can threshold -- an alert on it would fire and never clear -- so there is deliberately no
// Add form of this method, and TestDefaultPartitionRowsIsAGaugeAndNotACounter fails anyone who adds
// one. Its companion CountDefaultPartitionBlocked counts the *event*; this measures the *state*, and
// the state is the earlier and cheaper signal (ADR-8).
func (stats *MaintenanceStats) StoreDefaultPartitionRows(rows int64) {
	stats.defaultPartitionRows.Store(rows)
}

// DefaultPartitionRows is how many rows the DEFAULT partition held at the last pass that looked.
func (stats *MaintenanceStats) DefaultPartitionRows() int64 {
	return stats.defaultPartitionRows.Load()
}

// CountRetentionBlockedByLiveEvents records one range whose detach the composite foreign key refused
// because pending or delivering queue rows still reference it (ADR-9). A retention stalled this way
// grows disk indefinitely, and without its own counter it is indistinguishable from a retention pass
// that found nothing to do.
func (stats *MaintenanceStats) CountRetentionBlockedByLiveEvents() {
	stats.retentionBlockedByLiveEvents.Add(1)
}

// RetentionBlockedByLiveEvents is how many drops live queue rows have refused.
func (stats *MaintenanceStats) RetentionBlockedByLiveEvents() int64 {
	return stats.retentionBlockedByLiveEvents.Load()
}

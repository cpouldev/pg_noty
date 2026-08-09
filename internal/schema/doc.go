// Package schema is pg_noty's custody of its own storage inside a database it does not own: the six
// tables it creates, the forward-only ledger that creates them, the range partitions of the event
// table, and the arithmetic that decides which partitions must exist and which may be dropped.
//
// The dependency direction is one-way and asserted: this package reads internal/config and nothing
// in internal/config reaches back. Inside it, doc.go and partition.go are the L1 domain -- they hold
// no driver, no cancellation and no I/O, which is what lets every boundary decision be exercised in
// a sub-second loop with no container. l1imports_test.go declares that file set as a value and pins
// its size, so a new L1 file joining the package has to join the gate.
//
// Every decision this package makes about time is arithmetic on an epoch-UTC grid: boundaries are
// whole multiples of the configured partition interval from 1970-01-01T00:00:00Z, and `now` selects
// which of them must exist without ever anchoring one (ADR-6). Two boots minutes apart therefore
// demand the same boundaries, which matters because the server refuses overlapping ranges.
//
// # Reference
//
// The five things CONTRIBUTING.md has to state about this package, written here so it quotes
// them rather than paraphrasing them. Each is asserted against the code it describes by
// docreference_test.go, because a reference that has drifted is worse than none: it is published as
// the product's contract.
//
// # The object contract
//
// This package creates 6 tables, 1 DEFAULT partition and 4 indexes, all unqualified and all inside
// the configured service schema. The tables are schema_version, listeners, listener_triggers,
// events, event_queue and deliveries; the partition is events_default; the indexes are
// event_queue_claim_idx, event_queue_lease_idx, event_queue_listener_status_idx and
// deliveries_event_idx. Migration 1 creates schema_version alone, so the runner can read the ledger
// before applying anything else; migration 2 creates the rest. Objects below is the same list as a
// value, carrying each object's kind and the migration that creates it.
//
// # The alignment rule
//
// A partition's bounds are whole multiples of partition_interval counted from
// 1970-01-01T00:00:00Z, never from `now`. The set that must exist is the grid indexes k from
// floor(now/interval) through floor((now+precreate)/interval), inclusive at both ends: a bound is
// half-open, so an event occurring at exactly now+precreate belongs to the range that *starts*
// there and that range has to exist. Two replicas with skewed clocks inside one interval therefore
// ask for the same partitions, and a partition's name encodes both of its bounds so that two
// intervals over the same start are two names.
//
// # The status set
//
// event_queue.status is one of pending, delivering or dead, held to that set by a CHECK rather than
// by a Postgres enum -- an enum's ALTER TYPE ADD VALUE cannot run inside a transaction block, which
// would make the next status change unshippable under the one-migration-per-transaction rule.
// internal/delivery owns the transitions; a fourth status needs a migration here.
//
// # The retention granularity guarantee
//
// Retention drops whole partitions and never rows, so `keep` is a floor and not a deadline. An event
// is retained for at least keep and at most keep + partition_interval: its partition is dropped only
// once the partition's upper bound is at or before now-keep, and the event may sit anywhere inside
// that partition. Shortening the window an operator is exposed to therefore means shortening
// partition_interval, not keep.
//
// # The minimal grant set
//
// The whole privilege set this package needs, granted to the pg_noty role, is four statement
// families: ownership of the service schema, CREATE on the service schema, USAGE on the service
// schema, and TRIGGER on a target table -- the last once per distinct target. Nothing else, and
// there is no statement for the application role at all: that role reaches the event log only
// through the SECURITY DEFINER trigger functions internal/source creates in this schema as this
// role. GrantStatements emits exactly that set, and testdata/grants.golden is its byte-exact form.
package schema

// ObjectKind is what a declared name names. The kinds are distinguished because an index is written
// into the catalog differently from a table, and this package's own object inventory has to know
// which question to ask of each name without carrying per-object knowledge of its own.
type ObjectKind string

const (
	KindTable     ObjectKind = "table"
	KindPartition ObjectKind = "partition"
	KindIndex     ObjectKind = "index"
)

// The object names internal/source, internal/reconcile, internal/delivery and internal/cli compile
// against. They are unqualified: the schema they live in is configuration
// (Config.Database.Schema), and the only place it is interpolated is the runner's SET LOCAL
// search_path, so a name written here can never carry one.
const (
	// TableSchemaVersion is the forward-only ledger, keyed on version. It is created by migration
	// 1 alone, so the runner can read it before applying anything else (ADR-5, ADR-10).
	TableSchemaVersion = "schema_version"
	// TableListeners is one row per configured listener, with the specification hash the
	// reconciler compares against.
	TableListeners = "listeners"
	// TableListenerTriggers is one row per generated trigger, keyed on (listener, operation).
	TableListenerTriggers = "listener_triggers"
	// TableEvents is the append-only event log, PARTITION BY RANGE (occurred_at). Its partitions
	// are the ranges partition.go computes.
	TableEvents = "events"
	// TableEventQueue is the narrow delivery queue the claim path reads. It is deliberately not
	// partitioned: that is what keeps the claim query off the partitioned table.
	TableEventQueue = "event_queue"
	// TableDeliveries is one row per delivery attempt, written once and read only for forensics.
	TableDeliveries = "deliveries"

	// PartitionDefault is the permanent DEFAULT partition of TableEvents. It is created by
	// migration 2 rather than by a maintenance pass, because its permanence must depend on
	// nothing running.
	PartitionDefault = "events_default"

	// IndexQueueClaim is the claim query's only index: partial on status = 'pending', leading
	// next_attempt_at for the range scan, event_id breaking ties deterministically.
	IndexQueueClaim = "event_queue_claim_idx"
	// IndexQueueLeaseReclaim serves the lease-reclaim sweep, partial on status = 'delivering'.
	IndexQueueLeaseReclaim = "event_queue_lease_idx"
	// IndexQueueListenerStatus serves per-listener status listing and the dead-letter metric.
	IndexQueueListenerStatus = "event_queue_listener_status_idx"
	// IndexDeliveriesEvent serves the foreign-key-side lookup from an event to its attempts.
	IndexDeliveriesEvent = "deliveries_event_idx"
)

// Object is one database object this package creates, with the migration that creates it.
type Object struct {
	// Name is the unqualified object name, and is one of the constants above.
	Name string
	Kind ObjectKind
	// Migration is the version of the migration that creates this object: 1 for the ledger, 2 for
	// everything else (ADR-10).
	Migration int
}

// Objects is every database object this package creates. It is a value rather than a list written
// inline at a call site, so an inventory can quantify over it and a size pin can fail when it grows.
// doc_test.go reconciles it against the constants above in both directions; the object inventory
// that compares it against the migration DDL is this package's own, and reads Kind and Migration
// rather than knowing per-object what to expect.
var Objects = []Object{
	{Name: TableSchemaVersion, Kind: KindTable, Migration: 1},
	{Name: TableListeners, Kind: KindTable, Migration: 2},
	{Name: TableListenerTriggers, Kind: KindTable, Migration: 2},
	{Name: TableEvents, Kind: KindTable, Migration: 2},
	{Name: TableEventQueue, Kind: KindTable, Migration: 2},
	{Name: TableDeliveries, Kind: KindTable, Migration: 2},
	{Name: PartitionDefault, Kind: KindPartition, Migration: 2},
	{Name: IndexQueueClaim, Kind: KindIndex, Migration: 2},
	{Name: IndexQueueLeaseReclaim, Kind: KindIndex, Migration: 2},
	{Name: IndexQueueListenerStatus, Kind: KindIndex, Migration: 2},
	{Name: IndexDeliveriesEvent, Kind: KindIndex, Migration: 2},
}

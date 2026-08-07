// Package config loads pg_noty's declarative configuration file, rejects every
// mistake that is knowable without a database, reports all of them in one run at
// exact source positions, and hands the rest of the program one typed, fully
// defaulted value.
//
// Load and Parse are the only entry points. Parse is the testable core: it takes
// bytes plus a display filename, so nothing in the package's own test suite touches
// the filesystem or opens a connection.
package config

import "time"

// Config is a validated, fully defaulted configuration. Every optional key has been
// resolved to either its written value or its built-in default, so a consumer never
// has to decide what a zero value means.
//
// The defaults block does not appear here on purpose: it is a merge layer that the
// merge stage consumes in full, and exposing it would invite a consumer to re-apply
// a merge this package already performed.
type Config struct {
	// Version is the configuration format version, which must be 1.
	Version int
	// Instance scopes ownership of every database object pg_noty creates. It
	// defaults to Database.Schema and is embedded in both the notification channel
	// name and the ownership marker.
	Instance string
	// AutoReconcile is whether run may change the database by itself: create its
	// schema, apply migrations and install triggers at startup. It defaults to
	// false, so the mutations are the operator's to make with bootstrap and apply,
	// and run only verifies that they were.
	AutoReconcile bool
	Database      Database
	Worker        Worker
	Retention     Retention
	// Listeners is empty when the file declares an empty list, which is the
	// deliberate way to say that this instance manages nothing. An absent listeners
	// key is a diagnostic instead, because the two mean very different things to the
	// reconciler.
	Listeners []Listener
}

// Database is how pg_noty reaches the target database and where it keeps its own
// objects.
type Database struct {
	// URL is the connection string used for everything except listening.
	URL string
	// Schema is where pg_noty creates its own objects. It cannot start with pg_,
	// which the server reserves for system schemas.
	Schema string
	// ListenURL is an optional session-mode connection used only for LISTEN, because
	// transaction pooling breaks it. Empty means URL is used.
	ListenURL string
}

// Worker configures the delivery worker pool.
type Worker struct {
	Concurrency int
	BatchSize   int
	// PollInterval is the safety net behind the notification fast path.
	PollInterval time.Duration
	// LeaseTimeout is how long a claimed event may stay claimed before another
	// worker reclaims it. It must exceed every listener timeout, or a slow request
	// is delivered twice.
	LeaseTimeout time.Duration
	// DrainTimeout bounds how long shutdown waits for in-flight deliveries.
	DrainTimeout time.Duration
	// AllowedDestinationCIDRs exempts named ranges from the SSRF guard: exact prefixes, not a boolean.
	AllowedDestinationCIDRs []string
}

// Retention configures how long delivered events are kept and how their partitions
// are managed.
type Retention struct {
	// Keep is how long an event stays before its partition is dropped.
	Keep time.Duration
	// PartitionInterval is the span each partition covers.
	PartitionInterval time.Duration
	// Precreate is how far ahead partitions are created. It is load-bearing on the
	// user's write availability, because a missing partition fails their writes.
	Precreate time.Duration
}

// Listener is one named unit of configuration: what to watch, and where to send it.
//
// The trigger-affecting and delivery-affecting halves are deliberately separate
// structs. Only Trigger determines what database objects exist, so it is the exact
// subtree the reconciler hashes; rotating a signing secret therefore cannot propose
// replacing a trigger, and no secret can reach the registry through that hash.
type Listener struct {
	// Name is the stable identity used for reconciliation and for deriving object
	// names.
	Name string
	// Enabled false means the triggers are dropped while the registry row is kept.
	// A disabled listener is still validated in full.
	Enabled bool
	// Trigger is everything that determines the database objects: consumed by
	// trigger generation and by the reconciler's specification hash.
	Trigger TriggerSpec
	// Delivery is everything that determines how an event is delivered: consumed by
	// the delivery worker.
	Delivery DeliverySpec
}

// TriggerSpec is the trigger-affecting half of a listener.
type TriggerSpec struct {
	// Table is the schema-qualified target table, as written.
	Table string
	// Operations holds one entry per statement the listener reacts to, with that
	// statement's own filters. There is one trigger per operation, because a trigger
	// covering several statements cannot carry a WHEN clause.
	Operations    Operations
	Payload       Payload
	tablePosition Positioned
}

// DeliverySpec is the delivery-affecting half of a listener.
type DeliverySpec struct {
	Destination Destination
	Retry       Retry
	// Timeout bounds one delivery attempt.
	Timeout time.Duration
	// Concurrency caps in-flight deliveries for this listener alone, and cannot
	// exceed the worker pool.
	Concurrency int
}

// Operations is an ordered slice rather than a map so that neither rendered output
// nor a downstream specification hash can depend on map iteration order. Entries are
// ordered insert, update, delete.
type Operations []Operation

// Operation is one statement a listener reacts to, with the filters that apply to
// that statement only. A single top-level filter would silently not apply to every
// statement, which is why the filters live here.
type Operation struct {
	// Kind is "insert", "update" or "delete". The step that validates the operations
	// mapping names those spellings as constants, because it is the first code that
	// compares against them.
	Kind string
	// Columns narrows an update to changes of these columns. It is meaningful only
	// for an update.
	Columns []string
	// When is a raw SQL condition that becomes the trigger's WHEN clause.
	//
	// TRUST BOUNDARY: this text is passed to the database as SQL. Anyone who can
	// edit the configuration file can therefore execute arbitrary SQL inside the
	// transaction that writes to the target table. Environment interpolation is
	// permitted here, which extends that boundary to whoever sets the variable.
	When            string
	columnsPosition Positioned
	whenPosition    Positioned
}

// Payload describes what an event carries.
type Payload struct {
	// Mode is full, columns or keys_only.
	Mode string
	// Columns is the whitelist that mode columns requires.
	Columns []string
	// Exclude is the blacklist that mode full accepts, and exists so that a full
	// payload of a table holding secrets does not ship them to a third party.
	Exclude []string
	// IncludeOld adds the pre-change row to the payload.
	IncludeOld bool
	// MaxBytes is a hard cap. An event above it fails rather than being truncated.
	MaxBytes int
}

// Destination is where an event is delivered.
type Destination struct {
	URL string
	// Method is POST, PUT or PATCH.
	Method string
	// Headers are the user's headers, stored under canonical HTTP names. Names
	// reserved by pg_noty itself are rejected.
	Headers Headers
	Signing Signing
}

// Signing configures request signatures.
type Signing struct {
	// Secrets holds every active secret. Requests are signed with all of them, which
	// is what lets a secret rotate without downtime.
	//
	// These values are sensitive: they are never rendered into a diagnostic and
	// never included in a log representation.
	Secrets []string
}

// Retry is a delivery retry policy. It is resolved from the built-in defaults, then
// the defaults block, then the listener, field by field -- so a listener overriding
// one field inherits the rest.
type Retry struct {
	MaxAttempts int
	// Backoff is exponential, linear or fixed.
	Backoff         string
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Jitter          bool
}

// Headers maps canonical HTTP field names to their values. Names are canonicalised
// on the way in, so the author's capitalisation cannot change what is transmitted
// and two spellings of one name cannot both survive a merge.
type Headers map[string]string

package config

import "time"

// defaultState is the resolved root template plus the listener fields every listener starts from.
// It is internal because defaults are a merge layer, never part of the public Config tree.
type defaultState struct {
	database      Database
	worker        Worker
	retention     Retention
	instance      func(schema string) string
	autoReconcile bool
	listener      listenerDefaults
}

type listenerDefaults struct {
	enabled           bool
	timeout           time.Duration
	retry             Retry
	operationDistinct bool
	payload           Payload
	destinationMethod string
}

// builtInDefault is one row of the contract's complete default table. The setter keeps unlike Go
// types in one ordered enumeration without converting durations or booleans through strings.
type builtInDefault struct {
	path  string
	apply func(*defaultState)
}

// builtInDefaults is the single enumeration of all 23 built-in defaults. Instance is a deferred
// derivation: its row stores the relationship to the effective database schema, not "noty".
//
// The auto_reconcile row assigns false, which is Go's zero value and therefore a no-op, and it is
// written out for the same reason payload.include_old is: this table is the contract's default set,
// so a default that happens to coincide with a zero value still has to be stated once here or the
// enumeration stops being the place a reader can learn it.
var builtInDefaults = []builtInDefault{
	{"database.schema", func(d *defaultState) { d.database.Schema = suggestedSchema }},
	{"instance", func(d *defaultState) { d.instance = func(schema string) string { return schema } }},
	{"auto_reconcile", func(d *defaultState) { d.autoReconcile = false }},
	{"worker.concurrency", func(d *defaultState) { d.worker.Concurrency = 16 }},
	{"worker.batch_size", func(d *defaultState) { d.worker.BatchSize = 100 }},
	{"worker.poll_interval", func(d *defaultState) { d.worker.PollInterval = 10 * time.Second }},
	{"worker.lease_timeout", func(d *defaultState) { d.worker.LeaseTimeout = 5 * time.Minute }},
	{"worker.drain_timeout", func(d *defaultState) { d.worker.DrainTimeout = 30 * time.Second }},
	{"retention.keep", func(d *defaultState) { d.retention.Keep = 168 * time.Hour }},
	{"retention.partition_interval", func(d *defaultState) { d.retention.PartitionInterval = 24 * time.Hour }},
	{"retention.precreate", func(d *defaultState) { d.retention.Precreate = 168 * time.Hour }},
	{"timeout", func(d *defaultState) { d.listener.timeout = 5 * time.Second }},
	{"retry.max_attempts", func(d *defaultState) { d.listener.retry.MaxAttempts = 5 }},
	{"retry.backoff", func(d *defaultState) { d.listener.retry.Backoff = "exponential" }},
	{"retry.initial_interval", func(d *defaultState) { d.listener.retry.InitialInterval = 10 * time.Second }},
	{"retry.max_interval", func(d *defaultState) { d.listener.retry.MaxInterval = time.Hour }},
	{"retry.jitter", func(d *defaultState) { d.listener.retry.Jitter = true }},
	{"enabled", func(d *defaultState) { d.listener.enabled = true }},
	{"operations.update.is_distinct", func(d *defaultState) { d.listener.operationDistinct = false }},
	{"payload.mode", func(d *defaultState) { d.listener.payload.Mode = payloadModeFull }},
	{"payload.include_old", func(d *defaultState) { d.listener.payload.IncludeOld = false }},
	{"payload.max_bytes", func(d *defaultState) { d.listener.payload.MaxBytes = 262144 }},
	{"destination.method", func(d *defaultState) { d.listener.destinationMethod = "POST" }},
}

func resolvedBuiltInDefaults() defaultState {
	var defaults defaultState
	for _, entry := range builtInDefaults {
		entry.apply(&defaults)
	}
	return defaults
}

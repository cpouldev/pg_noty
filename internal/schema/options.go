package schema

import (
	"log/slog"
	"time"
)

// DefaultLockTimeout bounds how long any maintenance statement waits for a lock on noty.events
// before giving up. It is a Go-level constant rather than a YAML key because this package adds no
// configuration surface.
//
// Three seconds is short enough that a customer transaction is not visibly stalled -- both a
// partition create and a partition drop take ACCESS EXCLUSIVE on the parent, which blocks their
// inserts into unrelated partitions for as long as it is held -- and long enough to win an
// uncontended lock on a busy server.
//
// The value is pinned twice, from two sides, and changing it requires changing both:
// TestDefaultLockTimeoutIsTheValueContractsNames asserts this literal against the Contracts block,
// and Step 8 asserts the behaviour against a real lock_timeout holding a conflicting lock. Neither
// cites the other.
const DefaultLockTimeout = 3 * time.Second

// Options is how the lock timeout, the counters and the logger enter this package -- once, before
// either of the two entry points that consume them.
//
// It is declared here rather than beside its first consumer because it has four of them and they do
// not depend on one another: Steps 11 and 15 take it in their exported signatures
// (Maintain(ctx, pool, cfg, opts) and RepairDefaultPartition(ctx, pool, cfg, opts, rng)), and
// Steps 13 and 14 construct one internally. Left to whichever landed first it would have been
// invented twice.
//
// Every field is optional. A zero value is usable, and normalized is the single place that is made
// true -- see the note there before adding a default anywhere else.
type Options struct {
	// LockTimeout bounds every DDL statement the pass issues. Zero means DefaultLockTimeout.
	LockTimeout time.Duration
	// Stats is where the pass records what it observed. Nil means the observations are discarded.
	Stats *MaintenanceStats
	// Logger is where the pass writes. Nil means slog.Default().
	Logger *slog.Logger
}

// normalized is opts with every zero field replaced by the value the package uses in its place. It
// is a fixed point: normalising an already-normalised value returns it unchanged, so a consumer may
// call it without knowing whether an earlier one already did, and an explicitly configured
// LockTimeout is never overwritten by the default
// (TestNormalisingAnAlreadyNormalisedOptionsChangesNothing).
//
// This is the only place these defaults are applied. A caller applying its own is how the next
// caller comes to forget, and a nil Stats or Logger reaching an unmodified path is a crash in the
// maintenance loop of a running deployment rather than a test failure. Because it is the only place,
// MaintenanceStats carries no nil receiver guard of its own.
func (opts Options) normalized() Options {
	if opts.LockTimeout == 0 {
		opts.LockTimeout = DefaultLockTimeout
	}
	if opts.Stats == nil {
		// A counter set nobody else holds: the calls succeed and the numbers go nowhere, which is
		// what the caller asked for by passing none.
		opts.Stats = &MaintenanceStats{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return opts
}

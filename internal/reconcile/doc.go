// Package reconcile proves what the database holds and changes it only after proving that an
// object is its own. It is a library for planning and applying trigger reconciliation.
//
// Its L1→L4 layering keeps pure vocabulary and decisions in L1, use cases in L2, catalog and
// registry adapters in L3, and the plan/apply entry points in L4.
//
// Its ubiquitous language is listener, operation, pair, desired, recorded, observed, ownership
// proof, marker, fingerprint, action, plan, verdict, run lock, target, and live events.
//
// Deliberately absent are resource, state file, converge, sync, drift-free, job, task, message,
// range, reap, horizon, and plan maintenance.
//
// # Reference: locking
//
// Apply holds its run lock on one pinned connection. Creating a trigger takes SHARE ROW EXCLUSIVE and
// removing one takes ACCESS EXCLUSIVE; both operations use the bounded local lock timeout.
//
// # Reference: catalog search_path
//
// Every catalog decision reading pins search_path to the empty list:
//
//	SET LOCAL search_path = ''
//
// A WHEN clause must qualify names outside pg_catalog, so independent sessions read the same stored
// definitions. The setting is written above as an indented code block deliberately: a pair of bare
// apostrophes in running doc-comment text is rewritten by gofmt into a typographic quote, which
// would document a value PostgreSQL does not accept.
//
// # Reference: fingerprints
//
// A fingerprint covers pg_get_triggerdef and pg_get_functiondef readbacks, not generated text. It
// does not cover ACLs, comments (which are ownership provenance), or the target table itself.
//
// # Reference: verdicts
//
// Plan returns clean, changes_pending, or error; Apply returns structured results and never prompts.
//
// # Reference: ownership
//
// A missing, foreign, mismatched, or unregistered marker refuses the whole destructive run. Resolve
// the marker or registry disagreement before approving a destructive plan.
package reconcile

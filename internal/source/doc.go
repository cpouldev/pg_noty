// Package source is pg_noty's trigger compiler and delivery-source seam.
//
// The package is layered inward to outward. L1 holds closed vocabularies and pure naming; L2 holds
// SQL grammars and payload expressions; L3 assembles deterministic trigger DDL; L4 owns the
// EventSource adapter and its database lifecycle. Dependencies point inward toward those contracts:
// the generator does not open a connection, and only the adapter crosses the database boundary.
//
// # Reference
//
// These six sections are CONTRIBUTING.md's source. Each is held to the implementation by
// docreference_test.go, so the published reference cannot drift from the code it describes.
//
// # The when trust boundary
//
// DDL time execution installs the trigger as the privileged pg_noty role. row-write time execution
// runs inside the customer's transaction on the target table. internal/config's
// environment-variable interpolation extends the authority to whoever sets the referenced variable;
// parentheses in the generated WHEN clause provide precedence only, not security. A SQL parser is
// outside this package's dependency ceiling.
//
// # The measured overhead
//
// The committed regression ceiling is 1.7×. The measurement warms both arms, then times an
// untriggered and a triggered transaction of 10,000 rows four times in alternating order and
// reports the median of the four pair ratios. Contention lengthens whichever arm meets it, so a
// pair reports too high when the triggered arm met the load and too low when the baseline arm did;
// the median of four discards a pair from each tail, which is exactly those two shapes. The largest
// of the four is drawn from whichever pair met the busier machine, which is why the median is
// published rather than the maximum. On PostgreSQL 17.10 the highest median observed over repeated
// runs of the whole tagged suite is 1.674; a single unwarmed sample reads as low as 1.555, which is
// why both arms are warmed first. A run too loaded to complete three pairs inside its budget
// reports that it measured nothing rather than publishing a ratio. The recorded maximum median is
// 1.674, so the published ceiling is 1.7, the highest observed ratio rounded up on release
// hardware.
//
// # The ownership marker
//
// Ownership comments carry `pg_noty:v1:<instance>:<listener>:<operation>` with the long operation
// spelling. The maximum is 101 bytes (`11 + 41 + 1 + 41 + 1 + 6`), and it is comment text rather
// than a 63-byte identifier; `schema.MarkerPrefix` remains the one authority, so internal/reconcile
// can read every ownership field back intact.
//
// # The notification contract
//
// The transaction-local guard raises one notification per transaction regardless of row count.
// The guard key is instance-wide, so two listeners of one instance coalesce to one notification;
// two instances sharing a target suppress each other's notification through that same connection
// setting. The body uses `pg_notify`; events remain durable and polling remains the correctness
// guarantee.
//
// # The EventSource failure contract
//
// Claim with no work returns an empty result and nil. A late Ack, Nack or Dead returns
// ErrEventNotClaimed; an unavailable database remains an infrastructure error, and ErrSourceClosed
// means lifecycle ended. Ack removes the queue row, Nack schedules its retry instant, and Dead keeps
// the row dead; none writes the append-only event log. Close waits for the listener and dedicated
// connection but not in-flight operations. A lost LISTEN connection costs latency only.
//
// # The PgBouncer LISTEN caveat
//
// LISTEN is session state and transaction-pooling PgBouncer breaks it, so database.listen_url is
// the optional dedicated connection source. When it is empty the listener falls back to
// database.url; `listenURL` uses `pgx.Connect`, and the borrowed pool is never consumed for this
// long-lived connection.
//
// This file intentionally holds no declarations. Its comment owns the package's documentation
// budget, while the source population and artifact gates measure every later Go file.
package source

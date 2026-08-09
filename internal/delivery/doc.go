// Package delivery is pg_noty's outbound webhook worker.
//
// The worker consumes the frozen source.EventSource port for claim, outcome and wake-up
// transitions, and a sibling source.LeaseReclaimer for the independent expired-lease sweep. A
// claim transaction ends before any network request begins; the request and the outcome transition
// are separate operations, so an HTTP endpoint can never hold a queue row lock open.
//
// The package owns delivery policy and lifecycle, not queue schema. The source adapter owns SQL and
// database-clock semantics. In particular, lease reclaim is not a seventh EventSource method, and
// this package does not issue claim, acknowledgement, retry or dead-letter SQL itself.
//
// # Reference
//
// These sections are CONTRIBUTING.md's source. They are deliberately stated as contracts
// rather than implementation notes so the published reference can be checked against the worker.
//
// # Signing and body custody
//
// An event body is serialized once and reused byte-for-byte on every attempt. Each request signs
// timestamp + "." + body with HMAC-SHA256, writes the lowercase hexadecimal digest, and compares
// candidate digests with hmac.Equal when verification is needed. Attempt metadata belongs in headers,
// never in the signed body, so retries preserve the receiver's body-hash identity.
//
// # Retry scheduling
//
// Status classification, the retry ladder, full jitter, Retry-After and max_interval clamping are
// pure Go policy. The worker computes one absolute retry instant from its injected clock and passes
// that instant unchanged to Nack. The source adapter converts the relative difference to a
// database-clock-anchored interval; no worker timestamp is bound as the queue column value.
//
// # HTTP boundary
//
// A listener gets one explicitly configured client and transport, reused for all its requests.
// Dial, TLS, response-header and total-request timeouts are bounded, response bodies are drained and
// closed, and redirects return http.ErrUseLastResponse. The dialer's Control hook validates the
// already-resolved address immediately before connect, refusing loopback, private, link-local,
// metadata, multicast and IPv4-mapped equivalents. Hostname text alone is not an SSRF boundary.
//
// # Payload and attempts
//
// Payload size is checked before HTTP admission. A payload exactly at the configured limit is sent;
// one byte over is dead-lettered with a bounded reason and the original event remains complete.
// Response snippets are truncated in Go to 2 KiB before an outcome is recorded. The database does
// not enforce that bound with a CHECK: rejecting an oversized record would abort the outcome
// transaction, strand the row delivering until reclaim, and create an infinite redelivery loop.
//
// # Polling, admission and drain
//
// Polling is unconditional and is the correctness guarantee. LISTEN/NOTIFY is only a latency hint;
// its reconnect loop is independent of the fixed poll loop, and a disabled listener behaves the same.
// Per-listener concurrency uses a non-blocking buffered-channel semaphore, so one saturated
// destination cannot stall dispatch to another. Shutdown stops new claims, gives in-flight work an
// independent drain bound, releases this worker's leases on an exceeded bound, and uses
// context.WithoutCancel for the trailing outcome write.
//
// This package intentionally contains no raw SQL and no additional module dependency. Queue table
// names, server-clock arithmetic and migration contracts remain in internal/source and
// internal/schema; standard-library HTTP, cryptography, context and synchronization primitives are
// sufficient for the worker.
package delivery

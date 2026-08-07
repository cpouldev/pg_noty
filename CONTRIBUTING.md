# Contributing to pg_noty

Thanks for taking the time. This document covers building from source, the test tiers, and the
design references each package publishes. For using `pg_noty`, see [README.md](README.md).

## Requirements

- **Go 1.25** (the exact version is in `go.mod`)
- **Docker** with a working daemon -- only for the integration tier and the demo stack

## Getting started

```sh
go build ./...
go run ./cmd/pg_noty --help
```

## Tests

There are two tiers, and both are release gates.

```sh
go test -count=1 ./...                           # container-free, runs anywhere
go test -p=1 -tags=integration -count=1 ./...    # PostgreSQL via Testcontainers, needs Docker
```

`-p=1` matters for the tagged tier: it serialises packages so two of them cannot race for the same
Docker resources. CI runs it that way, so a run without it is not the run CI performs.

`-count=1` matters: without it Go serves cached results and a green run may say nothing about the
code you just changed.

Two traps worth knowing:

- `gofmt -l` **exits 0 while listing files.** Check its output, not its exit code.
- `go test -run <name>` **exits 0 when the name matches nothing.** Confirm the test actually ran.

## The full gate set

Everything below must pass before a change ships:

```sh
gofmt -l ./cmd ./internal        # must print nothing
go build ./...
go vet ./...
go vet -tags=integration ./...
go mod tidy -diff
go test -count=1 ./...
go test -p=1 -tags=integration -count=1 ./...
```

## Running the demo stack

The committed stack builds the image from your working tree, which is what you want while
developing -- published-image instructions for users are in the README.

```sh
GO_VERSION="$(grep '^go ' go.mod | awk '{print $2}')" docker compose up --build
```

It starts PostgreSQL seeded from `demo/seed.sql`, an echo receiver, and `pg_noty` configured by
`demo/listeners.yaml`. Tear it down with `docker compose down -v`.

## Project layout

| Path | Responsibility |
|---|---|
| `cmd/pg_noty/` | The binary. |
| `internal/cli/` | Cobra commands, exit codes, HTTP endpoints, Prometheus wiring. |
| `internal/config/` | YAML loading, validation and diagnostics. Offline -- reaches no database. |
| `internal/schema/` | Owns storage: migrations, partitions, retention, grants. |
| `internal/source/` | Trigger DDL generation and the queue. Implements `EventSource`. |
| `internal/reconcile/` | Plans and applies database changes; ownership rules. |
| `internal/delivery/` | The webhook worker: signing, retries, HTTP boundary, SSRF guard. |
| `internal/goartifact/` | Shared source-measurement helpers used by tests. |
| `demo/` | Runnable example. |

Dependencies point inward, and each package's `doc.go` states its boundary. Only `internal/cli`
imports Cobra and Prometheus; keep the five library packages free of both.

## Conventions

- Format changed files with `gofmt -w`.
- Exported identifiers in PascalCase, unexported in camelCase, files in short lowercase names.
- Tests are named `TestBehavior`, fuzz targets `FuzzInvariant`, and tagged files
  `*_integration_test.go` carrying `//go:build integration`.
- Fixtures live in each package's `testdata/`. Update golden files only through the test's own
  update command.
- A configuration key's *shape* -- its kind and its sensitivity -- is declared in
  `internal/config/schema.go` and nowhere else. Adding a key is still a wider change than that one
  file: see **Structural gates** below.

## Structural gates

Two invariants are enforced by something other than an ordinary unit test:

- **Migration checksums** -- migration files are hashed and the hash is recorded in the database
  ledger. Editing a shipped migration makes every already-migrated database refuse to start; add a
  new migration instead.
- **Counted inventories** -- a few sets are pinned by an equality on their size, so that joining the
  set is a decision rather than an omission.

Make the code change, run the package's own suite, and let the failures enumerate the rest:

| Adding this | A test fails until you update |
|---|---|
| a CLI command | the list **and** the size `10` in `internal/cli/commandinventory_test.go` |
| a configuration key | `schema.go`, `raw.go`, `config.go`, `merge.go`, the `builtInDefaults` table **and** its count in `defaults_test.go`, the pinned key list in `schemacases_test.go`, and the `everyKindOfValue` fixture in `decodepolicy_test.go` |
| an `internal/schema` sentinel | `packageSentinels`, its count and name list in `errors_test.go`, and a `sentinelDetail` row with its own count |

Prose is a separate matter, and nothing checks it. The same three changes also mean editing the
Commands table in `README.md`, the key table and the "all N enumerated built-in defaults" sentence in
`internal/config/doc.go`, and the "eight sentinels" sentence in `internal/schema/errors.go`. A green
suite says nothing about any of them, so do it in the same commit as the code.

`ident.go` and `whenlex.go` are the SQL identifier rules and the WHEN-clause lexer: the trust
boundary between a configuration file and SQL running as the `pg_noty` role. Nothing pins their
bytes, so treat a change to either as security-relevant and say so in the pull request. Their
behaviour is covered by `identifier_test.go`, `identifierrefusal_test.go`, `whenlex_test.go` and
`FuzzARangeNameIsInjectiveAndWithinTheIdentifierLimit`, plus integration tests that put hostile
names through a real server.

## Pull requests

Explain the behaviour and the risk, link the relevant issue, and list every verification command you
ran. Include CLI output when user-visible behaviour changes, and call out skipped integration tests
explicitly. Keep commits focused, with short sentence-case imperative subjects (`Add ...`,
`Fix ...`, `Close ...`).

## Releases

Releases are tag-driven. Pushing a `v*` tag builds and publishes the multi-architecture image to
Docker Hub as `cpoul/pg_noty`, tagged `latest`, the full version, `<major>.<minor>`, `<major>` and
`sha-<commit>`.

---

## Design references

The sections below are copied from the `doc.go` of the four packages named above. Nothing checks
that they still match, so when you change a `doc.go`, mirror it here in the same commit.

## Source references

### The when trust boundary

DDL time execution installs the trigger as the privileged pg_noty role. row-write time execution
runs inside the customer's transaction on the target table. internal/config's
environment-variable interpolation extends the authority to whoever sets the referenced variable;
parentheses in the generated WHEN clause provide precedence only, not security. A SQL parser is
outside this package's dependency ceiling.

### The measured overhead

The committed regression ceiling is 1.7×. The measurement warms both arms, then times an
untriggered and a triggered transaction of 10,000 rows four times in alternating order and
reports the median of the four pair ratios. Contention lengthens whichever arm meets it, so a
pair reports too high when the triggered arm met the load and too low when the baseline arm did;
the median of four discards a pair from each tail, which is exactly those two shapes. The largest
of the four is drawn from whichever pair met the busier machine, which is why the median is
published rather than the maximum. On PostgreSQL 17.10 the highest median observed over repeated
runs of the whole tagged suite is 1.674; a single unwarmed sample reads as low as 1.555, which is
why both arms are warmed first. A run too loaded to complete three pairs inside its budget
reports that it measured nothing rather than publishing a ratio. The recorded maximum median is
1.674, so the published ceiling is 1.7, the highest observed ratio rounded up on release
hardware.

### The ownership marker

Ownership comments carry `pg_noty:v1:<instance>:<listener>:<operation>` with the long operation
spelling. The maximum is 101 bytes (`11 + 41 + 1 + 41 + 1 + 6`), and it is comment text rather
than a 63-byte identifier; `schema.MarkerPrefix` remains the one authority, so internal/reconcile
can read every ownership field back intact.

### The notification contract

The transaction-local guard raises one notification per transaction regardless of row count.
The guard key is instance-wide, so two listeners of one instance coalesce to one notification;
two instances sharing a target suppress each other's notification through that same connection
setting. The body uses `pg_notify`; events remain durable and polling remains the correctness
guarantee.

### The EventSource failure contract

Claim with no work returns an empty result and nil. A late Ack, Nack or Dead returns
ErrEventNotClaimed; an unavailable database remains an infrastructure error, and ErrSourceClosed
means lifecycle ended. Ack removes the queue row, Nack schedules its retry instant, and Dead keeps
the row dead; none writes the append-only event log. Close waits for the listener and dedicated
connection but not in-flight operations. A lost LISTEN connection costs latency only.

### The PgBouncer LISTEN caveat

LISTEN is session state and transaction-pooling PgBouncer breaks it, so database.listen_url is
the optional dedicated connection source. When it is empty the listener falls back to
database.url; `listenURL` uses `pgx.Connect`, and the borrowed pool is never consumed for this
long-lived connection.

This file intentionally holds no declarations. Its comment owns the package's documentation
budget, while the source population and artifact gates measure every later Go file.

## Schema references

### The object contract

This package creates 6 tables, 1 DEFAULT partition and 4 indexes, all unqualified and all inside
the configured service schema. The tables are schema_version, listeners, listener_triggers,
events, event_queue and deliveries; the partition is events_default; the indexes are
event_queue_claim_idx, event_queue_lease_idx, event_queue_listener_status_idx and
deliveries_event_idx. Migration 1 creates schema_version alone, so the runner can read the ledger
before applying anything else; migration 2 creates the rest. Objects below is the same list as a
value, carrying each object's kind and the migration that creates it.

### The alignment rule

A partition's bounds are whole multiples of partition_interval counted from
1970-01-01T00:00:00Z, never from `now`. The set that must exist is the grid indexes k from
floor(now/interval) through floor((now+precreate)/interval), inclusive at both ends: a bound is
half-open, so an event occurring at exactly now+precreate belongs to the range that *starts*
there and that range has to exist. Two replicas with skewed clocks inside one interval therefore
ask for the same partitions, and a partition's name encodes both of its bounds so that two
intervals over the same start are two names.

### The status set

event_queue.status is one of pending, delivering or dead, held to that set by a CHECK rather than
by a Postgres enum -- an enum's ALTER TYPE ADD VALUE cannot run inside a transaction block, which
would make the next status change unshippable under the one-migration-per-transaction rule.
internal/delivery owns the transitions; a fourth status needs a migration here.

### The retention granularity guarantee

Retention drops whole partitions and never rows, so `keep` is a floor and not a deadline. An event
is retained for at least keep and at most keep + partition_interval: its partition is dropped only
once the partition's upper bound is at or before now-keep, and the event may sit anywhere inside
that partition. Shortening the window an operator is exposed to therefore means shortening
partition_interval, not keep.

### The minimal grant set

The whole privilege set this package needs, granted to the pg_noty role, is four statement
families: ownership of the service schema, CREATE on the service schema, USAGE on the service
schema, and TRIGGER on a target table -- the last once per distinct target. Nothing else, and
there is no statement for the application role at all: that role reaches the event log only
through the SECURITY DEFINER trigger functions internal/source creates in this schema as this
role. GrantStatements emits exactly that set, and testdata/grants.golden is its byte-exact form.

## Delivery references

### Signing and body custody

An event body is serialized once and reused byte-for-byte on every attempt. Each request signs
timestamp + "." + body with HMAC-SHA256, writes the lowercase hexadecimal digest, and compares
candidate digests with hmac.Equal when verification is needed. Attempt metadata belongs in headers,
never in the signed body, so retries preserve the receiver's body-hash identity.

### Retry scheduling

Status classification, the retry ladder, full jitter, Retry-After and max_interval clamping are
pure Go policy. The worker computes one absolute retry instant from its injected clock and passes
that instant unchanged to Nack. The source adapter converts the relative difference to a
database-clock-anchored interval; no worker timestamp is bound as the queue column value.

### HTTP boundary

A listener gets one explicitly configured client and transport, reused for all its requests.
Dial, TLS, response-header and total-request timeouts are bounded, response bodies are drained and
closed, and redirects return http.ErrUseLastResponse. The dialer's Control hook validates the
already-resolved address immediately before connect, refusing loopback, private, link-local,
metadata, multicast and IPv4-mapped equivalents. Hostname text alone is not an SSRF boundary.

### Payload and attempts

Payload size is checked before HTTP admission. A payload exactly at the configured limit is sent;
one byte over is dead-lettered with a bounded reason and the original event remains complete.
Response snippets are truncated in Go to 2 KiB before an outcome is recorded. The database does
not enforce that bound with a CHECK: rejecting an oversized record would abort the outcome
transaction, strand the row delivering until reclaim, and create an infinite redelivery loop.

### Polling, admission and drain

Polling is unconditional and is the correctness guarantee. LISTEN/NOTIFY is only a latency hint;
its reconnect loop is independent of the fixed poll loop, and a disabled listener behaves the same.
Per-listener concurrency uses a non-blocking buffered-channel semaphore, so one saturated
destination cannot stall dispatch to another. Shutdown stops new claims, gives in-flight work an
independent drain bound, releases this worker's leases on an exceeded bound, and uses
context.WithoutCancel for the trailing outcome write.

This package intentionally contains no raw SQL and no additional module dependency. Queue table
names, server-clock arithmetic and migration contracts remain in internal/source and
internal/schema; standard-library HTTP, cryptography, context and synchronization primitives are
sufficient for the worker.

## Reconcile references

### Reference: locking

Apply holds its run lock on one pinned connection. Creating a trigger takes SHARE ROW EXCLUSIVE and
removing one takes ACCESS EXCLUSIVE; both operations use the bounded local lock timeout.

### Reference: catalog search_path

Every catalog decision reading pins search_path to the empty list:

	SET LOCAL search_path = ''

A WHEN clause must qualify names outside pg_catalog, so independent sessions read the same stored
definitions. The setting is written above as an indented code block deliberately: a pair of bare
apostrophes in running doc-comment text is rewritten by gofmt into a typographic quote, which
would document a value PostgreSQL does not accept.

### Reference: fingerprints

A fingerprint covers pg_get_triggerdef and pg_get_functiondef readbacks, not generated text. It
does not cover ACLs, comments (which are ownership provenance), or the target table itself.

### Reference: verdicts

Plan returns clean, changes_pending, or error; Apply returns structured results and never prompts.

### Reference: ownership

A missing, foreign, mismatched, or unregistered marker refuses the whole destructive run. Resolve
the marker or registry disagreement before approving a destructive plan.


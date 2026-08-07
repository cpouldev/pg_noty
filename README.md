# pg_noty

[![Go version](https://img.shields.io/github/go-mod/go-version/cpouldev/pg_noty?logo=go)](https://go.dev/)
[![Build and tests](https://img.shields.io/github/actions/workflow/status/cpouldev/pg_noty/ci.yml?branch=main&label=build%20%26%20tests&logo=githubactions)](https://github.com/cpouldev/pg_noty/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/cpouldev/pg_noty?sort=semver)](https://github.com/cpouldev/pg_noty/releases)
[![Docker pulls](https://img.shields.io/docker/pulls/cpoul/pg_noty?logo=docker)](https://hub.docker.com/r/cpoul/pg_noty)
[![License: MIT](https://img.shields.io/github/license/cpouldev/pg_noty)](LICENSE)

Turn PostgreSQL row changes into signed webhooks, without writing a single line of application code.

You point `pg_noty` at a table, install the triggers with one command, and it watches the queue and
POSTs a signed JSON payload to your endpoint every time a row changes. It owns its own tables in a
schema you name. Your tables and your egress network stay outside that boundary. By default the
long-running worker changes nothing at all, so DDL stays a step you take deliberately.

- **At-least-once delivery** with retries, exponential backoff and a dead-letter status
- **HMAC-SHA256 signatures** with rotation support, so receivers can verify authenticity
- **Declarative config**: one YAML file describes every listener; `plan`/`apply` reconcile it like Terraform
- **Built-in observability**: Prometheus metrics, health endpoints, structured logs with secrets redacted

```sh
docker pull cpoul/pg_noty:latest
```

Images are published for `linux/amd64` and `linux/arm64`, tagged `latest`, the full version,
`<major>.<minor>` and `<major>`. Pin a major or minor tag in production.

---

## Quick start (5 minutes)

You need Docker with Compose. No Go toolchain, no repository checkout, no local PostgreSQL.

Create an empty directory and put these three files in it.

**`docker-compose.yml`**

```yaml
services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: noty
      POSTGRES_PASSWORD: noty
      POSTGRES_DB: noty
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U noty -d noty"]
      interval: 2s
      retries: 20
    volumes:
      - ./seed.sql:/docker-entrypoint-initdb.d/010-orders.sql:ro

  echo:
    image: mendhak/http-https-echo:41

  pg_noty:
    image: cpoul/pg_noty:latest
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      DATABASE_URL: postgres://noty:noty@postgres:5432/noty?sslmode=disable
      ORDER_WEBHOOK_URL: http://echo:8080/
      WEBHOOK_SECRET: demo-secret
    volumes:
      - ./listeners.yaml:/etc/pg_noty/listeners.yaml:ro
    ports:
      - "8080:8080"
```

**`seed.sql`** is the table `pg_noty` will watch. It must exist before `pg_noty` starts, because
installing a trigger on a table that is not there is a refusal, not a warning.

```sql
CREATE TABLE public.orders (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer text NOT NULL,
    total_cents integer NOT NULL
);
```

**`listeners.yaml`**

```yaml
version: 1
# Let pg_noty create its schema and install the trigger on start, so this quick start is one
# command. The default is false; see "Who changes your database" below before production.
auto_reconcile: true
database:
  url: ${DATABASE_URL}
  schema: noty
worker:
  # Compose puts containers on a private network, which the SSRF guard blocks by
  # default. Naming the prefix is the supported way to reach a destination there.
  # Docker usually allocates from 172.16.0.0/12; some hosts use 192.168.0.0/16.
  allowed_destination_cidrs: ["172.16.0.0/12", "192.168.0.0/16"]
  poll_interval: 1s
listeners:
  - name: orders
    table: public.orders
    operations:
      insert: {}
    destination:
      url: ${ORDER_WEBHOOK_URL}
      signing:
        secrets:
          - ${WEBHOOK_SECRET}
```

Start it:

```sh
docker compose up
```

### Make something happen

In a second terminal, insert a row:

```sh
docker compose exec postgres psql -U noty -d noty \
  -c "INSERT INTO public.orders (customer, total_cents) VALUES ('ada', 4200);"
```

Watch it arrive:

```sh
docker compose logs -f echo
```

You will see a POST carrying these four headers:

```
X-Pg-Noty-Event-Id: 1
X-Pg-Noty-Timestamp: 1700000000
X-Pg-Noty-Attempt: 1
X-Pg-Noty-Signature: v1=<hex digest>
```

and this body, where `data` holds the row exactly as the trigger captured it:

```json
{
  "pg_noty": "1",
  "id": 1,
  "listener": "orders",
  "table": { "schema": "public", "name": "orders" },
  "op": "insert",
  "occurred_at": "2026-08-07T12:34:56.789012Z",
  "txid": 848,
  "data": { "old": null, "new": { "id": 1, "customer": "ada", "total_cents": 4200 } }
}
```

`data.old` is populated for `update` and `delete` only when `payload.include_old` is set.

### Check on it

```sh
docker compose exec pg_noty /pg_noty status -f /etc/pg_noty/listeners.yaml
curl -s localhost:8080/healthz     # process is alive
curl -s localhost:8080/readyz      # database reachable, schema current, startup reconcile done
curl -s localhost:8080/metrics     # Prometheus metrics
```

### Clean up

```sh
docker compose down -v
```

---

## Running it for real

The image's entrypoint is `["/pg_noty", "run", "-f", "/etc/pg_noty/listeners.yaml"]`, so mounting
your configuration at that path is all it takes.

```sh
docker run --rm \
  -e DATABASE_URL -e ORDER_WEBHOOK_URL -e WEBHOOK_SECRET \
  -v "$PWD/listeners.yaml:/etc/pg_noty/listeners.yaml:ro" \
  -p 8080:8080 \
  cpoul/pg_noty:latest
```

Run a different subcommand by overriding the arguments:

```sh
docker run --rm -e DATABASE_URL \
  -v "$PWD/listeners.yaml:/etc/pg_noty/listeners.yaml:ro" \
  cpoul/pg_noty:latest plan -f /etc/pg_noty/listeners.yaml
```

The image is `gcr.io/distroless/static-debian12:nonroot` with a statically linked binary and stays
under 10 MiB per platform. It contains no shell, package manager or compiler.

### Who changes your database

By default `pg_noty run` **changes nothing**. It creates no schema, applies no migration and
installs no trigger. It reads the database, checks that it matches your configuration, and refuses
to start if it does not, naming the command that fixes it. You make the changes, under whatever role
and change process you use for DDL.

So a deploy is two writing steps and the workload, and only the writing steps need privileges the
worker does not. Print the exact grants and hand them to a DBA:

```sh
pg_noty grants -f listeners.yaml --role noty
```

```sh
pg_noty bootstrap -f listeners.yaml   # writes: creates its schema, tables and partitions
pg_noty plan -f listeners.yaml        # optional preview; exit 2 means changes are pending
pg_noty apply -f listeners.yaml       # writes: installs the triggers
pg_noty run -f listeners.yaml         # verifies, then serves
```

`bootstrap` and `apply` fit an init container or a migration job; `run` is the workload. Both
writing steps preview with `--dry-run`, so CI can gate on `bootstrap --dry-run` and `plan` before
anything is written. `run` refuses `--dry-run`, because it settles queue rows whatever else it does.

```sh
pg_noty bootstrap -f listeners.yaml --dry-run
schema: noty
applied_version: absent          # or the version the ledger records
expected_version: 2
```

It exits `2` when the two versions differ, the same "changes pending" status `plan` uses, and `0`
when the database already carries every migration this binary embeds.

To let `run` do all of it at startup instead, set the key:

```yaml
auto_reconcile: true
```

Or pass `--reconcile` for a single invocation, and `--reconcile=false` to override a file that turns
it on. The flag beats the file; the file beats the built-in `false`.

---

## Configuration

Everything `pg_noty` does is described by one YAML file, annotated here:

```yaml
version: 1                          # required, must be 1
auto_reconcile: false               # may run change the database itself? (default: false)

database:
  url: ${DATABASE_URL}              # required: env var, never a literal password
  schema: noty                      # where pg_noty creates ITS tables (default: noty)

worker:
  concurrency: 4                    # total in-flight deliveries, all listeners (default: 16)
  batch_size: 16                    # queue rows claimed per poll (default: 100)
  poll_interval: 1s                 # how often to poll (default: 10s)
  lease_timeout: 30s                # before a stalled delivery is reclaimed (default: 5m)
  drain_timeout: 10s                # grace period for in-flight work on shutdown (default: 30s)
  allowed_destination_cidrs: []     # SSRF exemptions, see below

listeners:
  - name: orders                    # required, unique
    enabled: true                   # default: true
    table: public.orders            # required, must be schema-qualified
    operations:
      insert: {}                    # fire on INSERT. Also: update, delete
    payload:
      mode: full                    # full | columns | keys_only (default: full)
    destination:
      url: ${ORDER_WEBHOOK_URL}     # required
      signing:
        secrets:
          - ${WEBHOOK_SECRET}       # every entry signs; any of them verifies
```

Validate a file without touching a database:

```sh
pg_noty validate -f listeners.yaml
```

It reports every problem at once, with line and column, and never prints a secret.

### Settings reference

**Top level**

| Key | Default | Notes |
|---|---|---|
| `version` | none | Required. Must be `1`. |
| `auto_reconcile` | `false` | Whether `run` may create its schema, apply migrations and install triggers at startup. `false` makes it verify and refuse instead. See [Who changes your database](#who-changes-your-database). |
| `instance` | value of `database.schema` | Names this deployment. Two instances sharing a database must differ. |
| `database` | none | Required. |
| `worker` | none | Delivery worker tuning. |
| `retention` | none | How long events are kept. |
| `defaults` | none | Values inherited by every listener. |
| `listeners` | none | Required. May be an empty list, which means "manage nothing". |

**`database`**

| Key | Default | Notes |
|---|---|---|
| `url` | none | Required. Connection URI or libpq keyword string. Any password is redacted from logs and errors. |
| `schema` | `noty` | Where `pg_noty` creates its own tables. May not start with `pg_`. |
| `listen_url` | none | Optional dedicated session connection for `LISTEN`. See the PgBouncer note below. |

**`worker`**

| Key | Default | Notes |
|---|---|---|
| `concurrency` | `16` | Total in-flight deliveries across every listener: one process-wide pool. To stop one slow destination monopolising it, cap that listener with `listeners[].concurrency`. |
| `batch_size` | `100` | Queue rows claimed per poll. |
| `poll_interval` | `10s` | Polling is the correctness guarantee; `LISTEN`/`NOTIFY` only lowers latency. |
| `lease_timeout` | `5m` | A delivery still running after this is reclaimed and retried. |
| `drain_timeout` | `30s` | Grace period for in-flight deliveries at shutdown. |
| `allowed_destination_cidrs` | none | Explicitly permitted destination ranges. See SSRF below. |

**`retention`**

| Key | Default | Notes |
|---|---|---|
| `keep` | `168h` (7 days) | A floor, not a deadline: retention drops whole partitions, never rows. |
| `partition_interval` | `24h` | Events are range-partitioned by time. |
| `precreate` | `168h` | How far ahead partitions are created. |

**Per listener.** The `defaults` block is not a copy of this table: it accepts exactly three keys,
`timeout`, `retry` and `headers`, and anything else written there is an unknown-key error. Note that
its header key is `defaults.headers`, while a listener's is `destination.headers`.

| Key | Default | Notes |
|---|---|---|
| `name` | none | Required, unique. Lowercase letters, digits and underscores, starting with a letter (`^[a-z][a-z0-9_]{0,40}$`). |
| `enabled` | `true` | `false` leaves the listener configured but installs no trigger. |
| `table` | none | Required, schema-qualified: `public.orders`, not `orders`. |
| `operations` | none | Required. A mapping with any of `insert`, `update`, `delete`. |
| `timeout` | `5s` | Per-request HTTP timeout. |
| `payload.mode` | `full` | `full` (whole row), `columns` (a named subset), `keys_only` (primary key only). |
| `payload.columns` | none | Required when `mode: columns`. |
| `payload.exclude` | none | Columns to omit. |
| `payload.include_old` | `false` | Include the pre-change row for `update` and `delete`. |
| `payload.max_bytes` | `262144` (256 KiB) | Larger payloads are dead-lettered, not silently truncated. |
| `destination.url` | none | Required. |
| `destination.method` | `POST` | `POST`, `PUT` or `PATCH`. |
| `destination.headers` | none | Extra request headers. Values are redacted in logs. `X-Pg-Noty-` is reserved. |
| `destination.signing.secrets` | none | List. **Every** entry signs (the header carries one `v1=` term per secret, in order) and any of them verifies. |
| `concurrency` | none | Caps in-flight deliveries for this listener alone, within `worker.concurrency`. Listener-level only; not settable under `defaults`. |
| `retry.max_attempts` | `5` | |
| `retry.backoff` | `exponential` | `exponential`, `linear` or `fixed`. |
| `retry.initial_interval` | `10s` | |
| `retry.max_interval` | `1h` | Ceiling for the computed delay. |
| `retry.jitter` | `true` | Full jitter, to avoid retry storms. |

Durations accept Go units: `300ms`, `10s`, `5m`, `168h`. Days are **not** a unit: write `168h`, not `7d`.

### Filtering: which rows fire

Each operation can carry a `when:` condition and its own column list:

```yaml
listeners:
  - name: big_orders
    table: public.orders
    operations:
      insert:
        when: "NEW.total_cents > 10000"
      update:
        columns: [status]                    # only fire when `status` changes
        when: "OLD.status IS DISTINCT FROM NEW.status"
    payload:
      mode: columns
      columns: [id, status, total_cents]
      include_old: true
    destination:
      url: ${BIG_ORDER_WEBHOOK}
```

`when:` is raw SQL spliced into the trigger's `WHEN` clause. It runs inside your write transaction,
so keep it cheap, and treat it as a trust boundary: anyone who can edit this file can run SQL as the
`pg_noty` role.

### Secrets and environment variables

Any value may reference the environment with `${VAR}`, and `${VAR:-fallback}` supplies a default.
Keep credentials out of the file itself:

```yaml
database:
  url: ${DATABASE_URL}
destination:
  url: ${ORDER_WEBHOOK_URL}
  signing:
    secrets: [${WEBHOOK_SECRET}]
```

`pg_noty` redacts `database.url` and `listen_url` passwords, `destination.url` passwords, every
`signing.secrets` entry and every header value from logs, errors and diagnostics, in both text and
JSON output. Writing a literal secret produces a warning telling you to use a variable instead.

### Rotating a signing secret

Add the new secret to the list and deploy. Requests are signed with **every** configured secret, so
receivers accepting either one keep working; remove the old entry once they have all moved.

```yaml
signing:
  secrets:
    - ${WEBHOOK_SECRET_NEW}
    - ${WEBHOOK_SECRET_OLD}
```

---

## Commands

| Command | What it does |
|---|---|
| `validate` | Check a configuration file. Never connects to PostgreSQL. |
| `bootstrap` | Create this instance's schema, tables and partitions. Installs no trigger. |
| `plan` | Show what `apply` would change. Takes no locks. |
| `apply` | Reconcile the database to the configuration. |
| `run` | Verify the database matches the configuration, then serve deliveries until stopped. |
| `status` | Show schema version, listeners and queue depth. |
| `events` | Inspect (`events list`) and requeue (`events retry`) queued events. |
| `grants` | Print the minimal PostgreSQL privilege statements. |
| `destroy` | Remove the objects this instance owns. |
| `repair-default-partition` | Drain rows that landed in the DEFAULT partition. |

**Global flags:** `--config`/`-f`, `--log-level`, `--log-format`, `--dry-run`, `--allow-delete`,
`--auto-approve`.

**Per command:** `apply` adds `--run-lock-wait`; `run` adds `--reconcile` and `--listen`;
`destroy` adds `--confirm`; `grants` adds `--role`; `events list` takes `--listener` and `--status`,
and `events retry` also takes `--id`; `repair-default-partition` takes `--from`, `--to` and `--name`.

Exit status is `0` for clean, `2` for changes pending and `1` for error, so CI can branch on a plan
that would change something:

```sh
pg_noty plan -f listeners.yaml
case $? in
  0) echo "no changes" ;;
  2) echo "drift detected" ;;
  *) echo "plan failed"; exit 1 ;;
esac
```

Three commands return `2`: `plan` for a pending change, `bootstrap --dry-run` for a schema behind
the binary, and `run` for a database whose schema is current but whose triggers have drifted. So a
supervisor sees `2`, not `1`, for a database that needs `apply`. A `run` that cannot find its schema
at all exits `1`.

### What gets printed where

A command that fails writes `error: <message>` to **stderr**. A refusal from the reconciler writes
its reason to stdout with the remedy indented beneath it:

```text
cannot reconcile: required bootstrap object schema "noty" is absent
  remedy: run bootstrap for this service schema before reconciling
```

Exit `2` is an outcome, not a fault, so a `plan` reporting pending changes prints **no** `error:`
line. Branch on the status, not on stderr.

---

## Operating it

### Endpoints

`/healthz` is process liveness only. `/readyz` checks database reachability, schema version and the
startup reconcile latch. The version check compares what the ledger records against what this binary
embeds, so a database behind or ahead of the running image is reported rather than counted as
present. `/metrics` is Prometheus.

### Metrics

| Metric | Use it for |
|---|---|
| `pg_noty_queue_depth` | Backlog, labelled by status. Alert on `{status="dead"}`. |
| `pg_noty_oldest_pending_seconds` | Delivery lag. |
| `pg_noty_dead_events_total` | Events that exhausted their retries. |
| `pg_noty_delivery_duration_seconds` | Destination latency. |
| `pg_noty_delivery_attempts_total` | Attempt count, including retries. |
| `pg_noty_partition_coverage_seconds` | How far ahead partitions exist. |
| `pg_noty_default_partition_rows` | Non-zero means a range was missed; see the runbook below. |
| `pg_noty_retention_blocked_total` | Retention could not drop a partition. |
| `pg_noty_reconcile_duration_seconds` | Reconcile cost. |
| `pg_noty_reconcile_errors_total` | Reconcile failures. |

`pg_noty_dead_events_total` is database-backed but counter-suffixed, so `increase()` is meaningful
only over a window in which no `events retry` run reset the live dead count. Alert on the level with
`pg_noty_queue_depth{status="dead"}` instead.

### Requeuing failed events

```sh
pg_noty events list -f listeners.yaml --status dead
pg_noty events retry -f listeners.yaml --listener orders --status dead
pg_noty events retry -f listeners.yaml --id 42
```

A selector form (`--listener`/`--status`) drains to completion by itself, printing `retried N` once
per 1000-row batch. The `--id` form resets that one event and prints a single count.

### Runbook: rows in the DEFAULT partition

When `pg_noty_default_partition_rows` is non-zero, read the maintenance log's
`RepairDefaultPartition` pointer and run `repair-default-partition --from ... --to ... --name ...`.
The operator invokes this command and the scheduler never runs it, because creating a partition
takes an exclusive lock on the parent and DEFAULT partition. The drain moves events and queue rows
atomically and reports the counts before and after.

### Costs to know about

`apply` takes a bounded local DDL lock of 3 seconds and waits at most 60 seconds for the instance run
lock. Replace is non-destructive; drop and disable require explicit approval. Partition maintenance
is load-bearing on write availability, so the maintenance pass detects a missing range and no timer
repairs it.

The trigger tax is one durable event and one queue row per accepted change. Enqueue is deliberately
fail-closed: doing it in an exception handler would start a subtransaction per row and overflow the
PostgreSQL per-session subxid cache past 64, degrading visibility checks for every backend.

### PgBouncer

Transaction pooling breaks `LISTEN`; set `database.listen_url` to a dedicated session URL or rely on
polling, which remains the correctness guarantee.

### Egress and SSRF

The worker resolves each destination at connect time and refuses loopback, private, link-local,
carrier-grade NAT, metadata, multicast and IPv4-mapped equivalents. To reach a destination inside one
of those ranges (a service on a container network, for instance), name the prefix explicitly:

```yaml
worker:
  allowed_destination_cidrs: ["172.16.0.0/12"]
```

Egress address filtering and DNS rebinding controls belong in the deployment network. Network policy
remains the operator's second boundary.

### Privileges

The minimal privilege model is ownership of the service schema, CREATE and USAGE on it, and TRIGGER
on each distinct target. No `pg_`-prefixed role is a valid service role, and the application role
needs no grant at all, because trigger functions run SECURITY DEFINER. Print the statements with
`pg_noty grants`.

---

## Receiving webhooks

Delivery is at-least-once and unordered. Deduplicate by `X-Pg-Noty-Event-Id`, and use the
`occurred_at`/`id` pair in the envelope as the durable ordering key:

```text
key := (envelope.occurred_at, envelope.id)
if already_seen(key) { acknowledge_without_side_effects() }
```

Verify `X-Pg-Noty-Signature` as `v1=<digest>,v1=<digest>` with constant-time comparison and a
timestamp tolerance of 300 seconds. Either configured secret may verify during rotation. Reject a
missing header, an unprefixed term, a wrong secret, or a timestamp outside `|skew| <= 300`.

The digest is `HMAC-SHA256(secret, "<timestamp>.<body>")`, lowercase hex. Both recipes below are
executed by this repository's test suite, so they cannot drift from the implementation.

### Go

<!-- BEGIN GO SIGNATURE RECIPE -->
```go
package main

import (
	"bytes"
	"math"

	"github.com/cpouldev/pg_noty/internal/delivery"
)

func verify(secret []byte, now, timestamp int64, body []byte, header string) bool {
	if math.Abs(float64(now-timestamp)) > 300 {
		return false
	}
	return delivery.VerifyHeader(secret, timestamp, body, header)
}

func main() {
	body, timestamp := []byte("body"), int64(1700000000)
	old, current := []byte("old"), []byte("secret")
	header := delivery.SignAll([][]byte{old, current}, timestamp, body)
	if !verify(current, timestamp, timestamp, body, header) || !verify(old, timestamp, timestamp, body, header) {
		panic("configured rotation did not verify")
	}
	if verify(current, timestamp, timestamp, []byte("tampered"), header) || verify([]byte("wrong"), timestamp, timestamp, body, header) {
		panic("tampered body or wrong secret verified")
	}
	if verify(current, timestamp+301, timestamp, body, header) || verify(current, timestamp-301, timestamp, body, header) {
		panic("out-of-window timestamp verified")
	}
	if !verify(current, timestamp+300, timestamp, body, header) || !verify(current, timestamp-300, timestamp, body, header) {
		panic("boundary timestamp rejected")
	}
	if delivery.VerifyHeader(current, timestamp, body, "") || delivery.VerifyHeader(current, timestamp, body, "bad") {
		panic("absent or unprefixed signature accepted")
	}
	if !bytes.Equal(body, []byte("body")) {
		panic("body changed")
	}
}
```
<!-- END GO SIGNATURE RECIPE -->

### Node.js

<!-- BEGIN NODE SIGNATURE RECIPE -->
```javascript
const crypto = require("crypto");
function digest(secret, timestamp, body) {
  return crypto.createHmac("sha256", secret).update(String(timestamp) + "." + body).digest("hex");
}
function verify(secret, now, timestamp, body, header) {
  if (!header || Math.abs(now - timestamp) > 300) return false;
  return header.split(",").every(term => term.startsWith("v1=")) && header.split(",").some(term => {
    const got = Buffer.from(term.slice(3), "hex");
    const want = Buffer.from(digest(secret, timestamp, body), "hex");
    return got.length === want.length && crypto.timingSafeEqual(got, want);
  });
}
const body = "body", timestamp = 1700000000, old = "old", current = "secret";
const header = `v1=${digest(old, timestamp, body)},v1=${digest(current, timestamp, body)}`;
if (!verify(current, timestamp, timestamp, body, header) || !verify(old, timestamp, timestamp, body, header)) process.exit(1);
if (verify(current, timestamp, timestamp, "tampered", header) || verify("wrong", timestamp, timestamp, body, header)) process.exit(1);
if (verify(current, timestamp + 301, timestamp, body, header) || !verify(current, timestamp + 300, timestamp, body, header)) process.exit(1);
if (verify(current, timestamp - 301, timestamp, body, header) || !verify(current, timestamp - 300, timestamp, body, header)) process.exit(1);
if (verify(current, timestamp, timestamp, body, "") || verify(current, timestamp, timestamp, body, "bad")) process.exit(1);
```
<!-- END NODE SIGNATURE RECIPE -->

---

## Troubleshooting

**`pg_noty validate` reports errors with line numbers.** Fix them top to bottom; every problem in the
file is reported in one pass, so you should not need to re-run more than once.

**Nothing arrives at my endpoint.** Check `pg_noty status` for queue depth, then
`pg_noty events list --status dead`. A destination in a private or carrier-grade NAT range, which
includes most container networks, is refused by the SSRF guard unless you list its prefix in
`worker.allowed_destination_cidrs`.

**`readyz` never turns healthy.** It checks three things: database reachability, schema version and
the startup reconcile latch. The 503 body names the failing one and why, so read it rather than the
logs: `curl -s localhost:8080/readyz`.

**Deliveries arrive twice.** That is by design: delivery is at-least-once. Deduplicate on
`X-Pg-Noty-Event-Id`.

**`apply` reports a run-lock timeout.** Another `pg_noty` process holds the instance lock. The
message names the instance and the holding backend; raise `--run-lock-wait` or stop the other process.

**A new listener is not firing.** Triggers are installed by `apply`, and `run` does not install them
unless `auto_reconcile` is on. Run `pg_noty apply` after adding a listener. A listener naming a table
that does not exist is refused outright rather than skipped; create the table first.

**`run` exits immediately saying the schema has not been bootstrapped.** That is the default: `run`
does not create anything. Run `pg_noty bootstrap` and `pg_noty apply`, or set `auto_reconcile: true`.

---

## Contributing

Building from source, the test tiers and each package's design references are in
[CONTRIBUTING.md](CONTRIBUTING.md).

---

## License

`pg_noty` is licensed under the [MIT License](LICENSE). See [NOTICE](NOTICE) for the licenses and
attributions of software included in the release binary.

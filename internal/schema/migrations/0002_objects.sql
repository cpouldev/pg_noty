-- Migration 2 creates every object phases 3 to 6 compile against: the five remaining tables, the
-- permanent DEFAULT partition of the event log, and the four indexes. Names are unqualified for the
-- reason recorded at the head of 0001_ledger (D1).
--
-- AC 39 is ratified -- ADR-9, Accepted, option (a) narrowed -- so this file ships one shape and not
-- a fork. The queue carries occurred_at and the composite foreign key below; "live" is narrowed to
-- pending and delivering, and rows already dead are reaped inside retention's own transaction.
-- Two costs were accepted with it and are recorded rather than buried. The queue is wider than the
-- design wanted, by exactly one column. And this phase declares the status vocabulary that phase 5
-- owns, which is a genuine inversion.
--
-- Growth path, so it is documented rather than rediscovered. Splitting this file into 0002 and 0003
-- later is permitted only while criterion 16's precondition survives -- it needs a ledger holding
-- versions 1 through K for some K strictly between 0 and N, which two migrations already satisfy
-- and one would not.

CREATE TABLE listeners (
    name text NOT NULL,
    spec jsonb NOT NULL,
    spec_hash text NOT NULL,
    target_table text NOT NULL,
    target_oid oid NOT NULL,
    enabled boolean NOT NULL,
    applied_at timestamptz NOT NULL,
    PRIMARY KEY (name)
);

COMMENT ON TABLE listeners IS
    'One row per configured listener. Phase 4 reconciles this against the configuration and owns every write to it; this phase creates it and leaves it empty.';

COMMENT ON COLUMN listeners.spec_hash IS
    'A hash of the listener specification as configured, so phase 4 can tell a changed configuration from an unchanged one without comparing whole documents.';

COMMENT ON COLUMN listeners.target_oid IS
    'The target table''s oid, which is what catches a rename. A renamed table keeps its triggers, so a name-based lookup alone would wrongly report the table dropped.';

CREATE TABLE listener_triggers (
    listener text NOT NULL,
    operation text NOT NULL,
    trigger_name text NOT NULL,
    function_name text NOT NULL,
    ddl_hash text NOT NULL,
    PRIMARY KEY (listener, operation),
    FOREIGN KEY (listener) REFERENCES listeners (name) ON DELETE CASCADE
);

COMMENT ON TABLE listener_triggers IS
    'One row per generated trigger, keyed on listener and operation, because the design generates one trigger per operation rather than one per listener -- so up to three rows per listener are representable. The cascade is what stops phase 4 orphaning trigger rows by removing a listener.';

COMMENT ON COLUMN listener_triggers.ddl_hash IS
    'A hash of the exact CREATE TRIGGER text generated, so phase 4 compares it against a normalised pg_get_triggerdef and catches a hand-edited trigger rather than only a changed configuration.';

-- The primary key is composite because the server forces it, not because the design chose it.
-- Measured on PostgreSQL 17.10, both ADD PRIMARY KEY (id) and ADD UNIQUE (id) are refused on a
-- partitioned table with "unique constraint on partitioned table must include all partitioning
-- columns" (M1). Two consequences the later phases inherit. First, id is unique only by
-- construction, through the single identity sequence, and never by constraint -- measured, a write
-- placing a second row with the same id in another partition succeeds and nothing objects, so no
-- phase may rely on a one-to-one mapping the catalog does not enforce. Second, a single-column
-- foreign key to this table is impossible, which is why the queue below carries occurred_at.
CREATE TABLE events (
    id bigint NOT NULL GENERATED ALWAYS AS IDENTITY,
    listener text NOT NULL,
    table_name text NOT NULL,
    operation text NOT NULL,
    payload jsonb NOT NULL,
    txid xid8 NOT NULL,
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (id, occurred_at)
) PARTITION BY RANGE (occurred_at);

COMMENT ON TABLE events IS
    'The append-only event log, range-partitioned on occurred_at. It carries no status, attempts or next_attempt_at column, and that absence is the design rather than an omission -- the mutable delivery state lives in the queue, which is what keeps the claim path off this partitioned table and keeps a retry from rewriting a payload.';

COMMENT ON COLUMN events.txid IS
    'The transaction that produced the event, from pg_current_xact_id. It is nearly free at write time and lets a consumer group the events of one transaction, which is the feature most often missing from webhook change-capture tools.';

COMMENT ON COLUMN events.occurred_at IS
    'The partitioning column. Boundaries are whole multiples of the configured interval counted from the Unix epoch in UTC, so two boots minutes apart demand the same boundaries and the server''s refusal of overlapping ranges cannot be met.';

-- The DEFAULT partition is created here, by the migration, and never by a maintenance pass:
-- criterion 29 requires its permanence to depend on nothing running. Created by maintenance it
-- would be absent on every database where maintenance has not yet succeeded, and its absence turns
-- each write with no covering range into "no partition of relation events found for row" inside the
-- customer's own transaction -- which is the availability failure this whole design exists to
-- avoid.
CREATE TABLE events_default PARTITION OF events DEFAULT;

COMMENT ON TABLE events_default IS
    'The permanent safety net, so a maintenance failure degrades to rows landing here rather than to the customer''s writes breaking. Measured, it cannot self-heal -- once a row for some range lands here, creating that range''s partition is refused with "updated partition constraint for default partition would be violated by some row" until those rows are moved out. Draining it is therefore an operator-invoked repair and never an automatic one, because the drain holds a lock strong enough to stall the application it is protecting.';

-- status is text with a CHECK and deliberately not a Postgres enum. ALTER TYPE ADD VALUE cannot
-- have its new value referenced until the adding transaction commits, which fights the forward-only
-- one-migration-per-transaction rule and would make the next status change unshippable. Three
-- values are the whole vocabulary -- delivered is the queue row's absence, and failed is pending
-- with attempts above zero.
--
-- occurred_at is here for one reason and no other, which is that it composes the foreign key below.
-- The key is the point of the ratified answer. Measured, DETACH PARTITION is refused with
-- "removing partition ... violates foreign key constraint ..." while any queue row still points
-- into that partition, so the refusal to drop a partition holding an undelivered event is the
-- server's and not our Go guard's -- a retention bug cannot destroy such an event even when the
-- application-level guard is wrong. That is the property no other option could offer, since neither
-- of the two rejected ones can carry a foreign key at all.
CREATE TABLE event_queue (
    event_id bigint NOT NULL,
    occurred_at timestamptz NOT NULL,
    listener text NOT NULL,
    status text NOT NULL,
    attempts int NOT NULL,
    next_attempt_at timestamptz NOT NULL,
    leased_until timestamptz NULL,
    leased_by text NULL,
    dead_reason text NULL,
    PRIMARY KEY (event_id),
    CHECK (status IN ('pending', 'delivering', 'dead')),
    FOREIGN KEY (event_id, occurred_at) REFERENCES events (id, occurred_at)
);

COMMENT ON TABLE event_queue IS
    'The narrow delivery queue the claim path reads. It is deliberately not partitioned -- keeping it out of the partitioned table is what makes the claim query cheap and what makes partition maintenance safe to run beside it.';

COMMENT ON COLUMN event_queue.occurred_at IS
    'Here to compose the foreign key to the event log and for no other reason, which is the cost AC 39''s ratified answer accepted against keeping this queue narrow. Phase 5 must populate it with the event''s own occurred_at on every enqueue -- a mismatched value points the key at a different partition and the insert fails inside the customer''s transaction. The key is what makes retention''s refusal to drop a partition holding live events the server''s rather than our Go guard''s.';

COMMENT ON COLUMN event_queue.status IS
    'One of pending, delivering or dead, held to that set by a CHECK rather than by an enum. Phase 5 owns the transitions and must not add a fourth status without a migration here. Moving a row to dead releases its partition for retention, so dead is a decision to permit the event''s eventual deletion and not merely a report that delivery failed.';

CREATE TABLE deliveries (
    event_id bigint NOT NULL,
    attempt int NOT NULL,
    http_status int NULL,
    response_snippet text NULL,
    error text NULL,
    duration_ms int NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (event_id, attempt)
);

COMMENT ON TABLE deliveries IS
    'One row per delivery attempt, written once and read only for forensics. It declares no foreign key to the event log, for two reasons -- a single-column key to the log is refused by the server, and composing one would mean carrying occurred_at here as well, which the queue pays for a retention policy that forensics does not have.';

COMMENT ON COLUMN deliveries.http_status IS
    'Null when the attempt produced no status at all -- a transport error, a timeout, a name-resolution failure -- because phase 5 must still record that the attempt happened. The nullability is deliberate and is not an oversight.';

COMMENT ON COLUMN deliveries.response_snippet IS
    'Truncated to 2 KiB by phase 5 at write time. The cap is recorded here and is deliberately not enforced by a CHECK -- a rejected insert would abort phase 5''s record transaction, leaving the queue row delivering until lease reclaim, and would turn a truncation bug into an infinite redelivery loop. Failing closed is right for enqueue and wrong for forensics.';

CREATE INDEX event_queue_claim_idx ON event_queue (next_attempt_at, event_id) WHERE status = 'pending';

COMMENT ON INDEX event_queue_claim_idx IS
    'The claim query''s only index. Partial on pending so delivering and dead rows never enter it; leading next_attempt_at serves the range scan against the current time; event_id breaks ties deterministically and keeps the claim ordering satisfied from the index, so the plan carries no sort node. The index is covering, but the claim query takes FOR UPDATE SKIP LOCKED and row locking needs the heap tuple, so its plan is a row-locking node over an index scan and never an index-only scan.';

CREATE INDEX event_queue_lease_idx ON event_queue (leased_until) WHERE status = 'delivering';

COMMENT ON INDEX event_queue_lease_idx IS
    'The lease reclaim sweep. Partial on delivering, which keeps it small because a delivering row is transient by design.';

CREATE INDEX event_queue_listener_status_idx ON event_queue (listener, status);

COMMENT ON INDEX event_queue_listener_status_idx IS
    'Per-listener status listing and the dead-letter metric. It is deliberately not partial, because it answers questions about every status rather than about one of them.';

CREATE INDEX deliveries_event_idx ON deliveries (event_id);

COMMENT ON INDEX deliveries_event_idx IS
    'Lookup from an event to its attempts, which is the only way this table is read. It stands in for the key-side index a foreign key would have implied, and there is no foreign key here for the reason recorded on the table itself.';

-- Migration 1 creates the ledger and nothing else, so the runner can read what has already been
-- applied before it applies anything (ADR-5, ADR-10). Splitting the corpus in two is also what
-- gives criterion 16 a shipped corpus it can exercise, since it needs a ledger holding versions 1
-- through K for some K strictly between 0 and N, and a single-file corpus offers no such K.
--
-- Every object name in this corpus is unqualified, deliberately. The runner issues
-- SET LOCAL search_path as the migration transaction's first statement, and that is the only
-- interpolation site in the whole migration path (D1). A placeholder here would put an
-- interpolation inside text that is also checksummed, at which point "the checksum covers what was
-- executed" stops being true.

CREATE TABLE schema_version (
    version int NOT NULL,
    checksum text NOT NULL,
    applied_at timestamptz NOT NULL,
    PRIMARY KEY (version)
);

COMMENT ON TABLE schema_version IS
    'One row per applied version rather than one mutable row. With a single integer, "version 7 was applied once" and "version 7 was applied twice" are the same observation, and this service runs N replicas. The primary key on version is the mechanism and not bookkeeping -- a second concurrent application of a version cannot produce a second row to be noticed later, it raises a unique violation inside that migration''s own transaction and rolls the migration back.';

COMMENT ON COLUMN schema_version.version IS
    'The version the migration file''s own name declares, so the order the corpus applied in is auditable without reading any SQL. The recorded version of the database is the maximum over this column.';

COMMENT ON COLUMN schema_version.checksum IS
    'sha256 over the exact bytes of the migration file this row records, which is what makes a file edited after it was already applied detectable rather than silent. Flyway''s schema history table is the prior art. Because the corpus carries no interpolation, these bytes are exactly the bytes that were executed.';

COMMENT ON COLUMN schema_version.applied_at IS
    'When this version was applied. It is written from the database clock rather than a replica''s, so replicas with skewed clocks cannot disagree about the ledger.';

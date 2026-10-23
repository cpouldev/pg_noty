package schema

// Shared fixture settings used by the plain and integration tests.

const (
	// harnessImage is pinned to a patch release rather than to the floating 17 tag, because every
	// measurement in this task file was taken on PostgreSQL 17.10 and a tag that moved to 18 would
	// change what those pins mean without changing a line of Go.
	harnessImage = "postgres:17.10-alpine"
	// harnessDatabase is deliberately not the postgres system database (SC-4). Restore drops and
	// recreates its target, which cannot be done to the database the runner is connected through --
	// and modules/postgres defaults POSTGRES_DB to the user name, so a harness that does not say
	// otherwise gets `postgres` and every Restore fails with what reads as a harness bug.
	harnessDatabase = "notyharness"
	harnessUser     = "noty"
	harnessPassword = "noty-harness-secret"
	// harnessSchema is where the eight consuming steps create their objects.
	harnessSchema = "noty"
	// harnessSQLDriver is the database/sql driver name modules/postgres opens its snapshot and
	// restore connections with. It is `pgx` because pgx/v5/stdlib registers that name; without the
	// blank import the open fails and the module falls back to `docker exec psql`.
	harnessSQLDriver = "pgx"
)

package cli

import (
	"context"
	"errors"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// schemaState is what a caller forbidden from bootstrapping can learn about the service schema
// without writing to it: whether the ledger exists at all, and how far behind this binary it is.
//
// Everything that has to answer "may I proceed against this schema" reads it here rather than
// calling AppliedVersion and interpreting the error itself: bootstrap (both its --dry-run report and
// the version it reports afterwards), run's verification when auto_reconcile is off, the /readyz
// version condition, and status. A second interpretation would drift the moment one of them learned
// to tell "absent" from "behind" and the others did not -- which is the state this package was in
// before, when /readyz called AppliedVersion and discarded the number.
type schemaState struct {
	applied  int
	expected int
	present  bool
}

// current reports whether the database carries every migration this binary embeds. An absent ledger
// is not current, which is the answer that keeps a caller from serving against a schema that holds
// none of its tables.
func (state schemaState) current() bool { return state.present && state.applied == state.expected }

// readSchemaState reads the ledger and writes nothing. An absent ledger is returned as a state
// rather than as an error, because a database nothing has bootstrapped is the ordinary case both
// callers exist to report; every other failure is still an error, so a broken connection cannot be
// mistaken for a database awaiting its first migration.
func readSchemaState(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, opts schema.Options) (
	schemaState,
	error,
) {
	expected, err := schema.ExpectedVersion()
	if err != nil {
		return schemaState{}, err
	}
	applied, err := schema.AppliedVersion(ctx, pool, cfg, opts)
	if errors.Is(err, schema.ErrLedgerAbsent) {
		return schemaState{expected: expected}, nil
	}
	if err != nil {
		return schemaState{}, err
	}
	return schemaState{applied: applied, expected: expected, present: true}, nil
}

package reconcile

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// A when clause has four distinct authorities: privileged DDL-time trigger creation, privileged
// validation analysis here, customer row-write execution inside that customer's transaction, and
// internal/config interpolation, which extends authority to whoever sets the referenced variable.
// Reference: a when clause must qualify every name outside pg_catalog because validation pins no
// search_path.
//
// Prepare sends the extended protocol's Parse and Describe messages, so PostgreSQL analyses and
// rewrites without planning or executing. Its one-statement restriction is deliberately opposite to
// the batching rule applytx.go follows: apply must issue a when-bearing DDL statement alone because
// pgx uses simple protocol with no arguments, while this probe must make the server refuse a second
// statement.
func checkWhenParses(ctx context.Context, tx pgx.Tx, catalog Catalog, check config.DeferredCheck) (
	config.Error,
	bool,
	error,
) {
	target, found, name, err := resolveDeferredTarget(ctx, catalog, check)
	if err != nil {
		return config.Error{}, false, err
	}
	if !found {
		return deferredDiagnostic(check, "target table "+name+" does not exist"), true, nil
	}
	qualified, fault := schema.Qualified(target.Schema, target.Table)
	if fault != schema.IdentifierOK {
		return config.Error{}, false, fmt.Errorf("resolved target is unusable: %s", fault)
	}
	_, err = tx.Conn().PgConn().Prepare(ctx, "", whenProbe(qualified, qualified, check.When), nil)
	if err == nil {
		return config.Error{}, false, nil
	}
	var server *pgconn.PgError
	if errors.As(err, &server) {
		return deferredDiagnostic(
			check,
			"when clause is not valid for target table "+name+": "+server.Message,
		), true, nil
	}
	return config.Error{}, false, fmt.Errorf("analyse when clause: %w", err)
}

// whenProbe is this package's only raw SQL interpolation site. Its inputs are quoted catalog names
// and the expression internal/config deliberately approved raw; do not move either interpolation
// elsewhere.
func whenProbe(oldTarget, newTarget, clause string) string {
	return fmt.Sprintf("SELECT 1 FROM %s AS OLD, %s AS NEW WHERE (%s)", oldTarget, newTarget, clause)
}

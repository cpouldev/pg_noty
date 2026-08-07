package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

type readinessCondition struct {
	name    string
	timeout time.Duration
	check   func(context.Context) error
}

type readiness struct{ conditions []readinessCondition }

func newReadiness(pool *pgxpool.Pool, cfg config.Config, latch *reconcileLatch, logger *slog.Logger) readiness {
	return readiness{
		conditions: []readinessCondition{
			{name: "reachable", timeout: time.Second, check: func(ctx context.Context) error { return pool.Ping(ctx) }},
			{
				name: "schema version current", timeout: time.Second, check: func(ctx context.Context) error {
					return schemaVersionIsCurrent(ctx, pool, cfg, logger)
				},
			},
			{
				name: "reconcile discharged", timeout: time.Second, check: func(context.Context) error {
					if !latch.ready() {
						return fmt.Errorf("reconcile obligation is not discharged")
					}
					return nil
				},
			},
		},
	}
}

// schemaVersionIsCurrent answers the condition by its own name. Reading AppliedVersion and
// discarding the number would make "schema version current" true of any database holding a ledger,
// including one several migrations behind this binary and one migrated ahead of it by a newer
// replica. Both are states a load balancer should route away from, so both are reported.
//
// The wording names both versions and presumes nothing about the cause, because readiness cannot
// tell a database awaiting its first bootstrap from one a rollback left behind.
func schemaVersionIsCurrent(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, logger *slog.Logger) error {
	state, err := readSchemaState(ctx, pool, cfg, schemaOptions(logger))
	if err != nil {
		return err
	}
	switch {
	case !state.present:
		return fmt.Errorf("schema %s holds no migration ledger", cfg.Database.Schema)
	case state.applied != state.expected:
		return fmt.Errorf(
			"schema %s records version %d and this binary embeds version %d",
			cfg.Database.Schema, state.applied, state.expected,
		)
	}
	return nil
}

func (ready readiness) handler() http.Handler {
	return http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			for _, condition := range ready.conditions {
				ctx, cancel := context.WithTimeout(request.Context(), condition.timeout)
				err := condition.check(ctx)
				cancel()
				if err != nil {
					response.WriteHeader(http.StatusServiceUnavailable)
					_, _ = response.Write([]byte(condition.name + ": " + err.Error() + "\n"))
					return
				}
			}
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte("ready\n"))
		},
	)
}

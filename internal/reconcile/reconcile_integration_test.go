//go:build integration

package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5"
)

func TestPlanIssuesNoWriteBecauseTheServerRefusesOne(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	_, err := planWith(
		t.Context(),
		pool,
		harnessConfig(t),
		Options{},
		func(ctx context.Context, tx pgx.Tx, _ config.Config, _ *slog.Logger) (PlanResult, error) {
			_, err := tx.Exec(ctx, "CREATE TABLE public.plan_read_only_refusal (id int)")
			if err == nil {
				t.Fatal("server accepted a write in Plan's read-only transaction")
			}
			return PlanResult{}, nil
		},
	)
	if err != nil {
		t.Fatalf("plan transaction wrapper: %v", err)
	}
}

func TestPlanSnapshotDoesNotSeeAMidRunCommit(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	entered, release := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := planWith(
			t.Context(),
			pool,
			harnessConfig(t),
			Options{},
			func(ctx context.Context, tx pgx.Tx, _ config.Config, _ *slog.Logger) (PlanResult, error) {
				if err := tx.QueryRow(ctx, "SELECT count(*) FROM pg_catalog.pg_class").Scan(new(int)); err != nil {
					return PlanResult{}, err
				}
				close(entered)
				<-release
				var found bool
				err := tx.QueryRow(
					ctx,
					"SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_class WHERE relname = 'plan_snapshot_commit')",
				).Scan(&found)
				if err != nil || found {
					return PlanResult{}, fmt.Errorf("snapshot saw concurrent commit: found=%t err=%v", found, err)
				}
				return PlanResult{}, nil
			},
		)
		result <- err
	}()
	<-entered
	mustExecOn(t, pool, "CREATE TABLE public.plan_snapshot_commit (id int)")
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestPlanLeavesTheRunLockFreeForAnObserver(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	observer, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Release()
	key := schema.ReconcileLockKey(harnessSchema, harnessSchema)
	_, err = planWith(
		t.Context(),
		pool,
		harnessConfig(t),
		Options{},
		func(ctx context.Context, _ pgx.Tx, _ config.Config, _ *slog.Logger) (PlanResult, error) {
			var held bool
			if err := observer.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&held); err != nil || !held {
				t.Fatalf("observer lock result = %t, %v; want free key", held, err)
			}
			mustExecOn(t, observer, "SELECT pg_advisory_unlock($1)", key)
			return PlanResult{}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
}

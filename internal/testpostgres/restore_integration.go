//go:build integration

package testpostgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	snapshotRestoreAttempts = 3
	snapshotRestoreDelay    = 50 * time.Millisecond
)

type snapshotOperation func(context.Context) error
type snapshotDelay func(context.Context, time.Duration) error

// RestoreSnapshot restores a Testcontainers PostgreSQL snapshot and verifies that the application
// database accepts a connection before returning. Testcontainers implements restore as separate
// DROP DATABASE and CREATE DATABASE commands, so the narrowly classified retries cover the
// transient catalog states those commands can expose without hiding permanent fixture failures.
func RestoreSnapshot(ctx context.Context, container *postgres.PostgresContainer) error {
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return fmt.Errorf("read PostgreSQL container connection string: %w", err)
	}
	return restoreSnapshot(
		ctx,
		func(ctx context.Context) error { return container.Restore(ctx) },
		func(ctx context.Context) error {
			connection, err := pgx.Connect(ctx, dsn)
			if err != nil {
				return err
			}
			return connection.Close(ctx)
		},
		waitForSnapshotRetry,
	)
}

func restoreSnapshot(ctx context.Context, restore, verify snapshotOperation, delay snapshotDelay) error {
	var lastErr error
	for attempt := 1; attempt <= snapshotRestoreAttempts; attempt++ {
		if err := restore(ctx); err != nil {
			lastErr = fmt.Errorf("restore PostgreSQL snapshot: %w", err)
		} else if err := verify(ctx); err != nil {
			lastErr = fmt.Errorf("verify restored PostgreSQL database: %w", err)
		} else {
			return nil
		}

		if !transientSnapshotError(lastErr) || attempt == snapshotRestoreAttempts {
			return lastErr
		}
		if err := delay(ctx, time.Duration(attempt)*snapshotRestoreDelay); err != nil {
			return fmt.Errorf("wait to retry PostgreSQL snapshot restore: %w", err)
		}
	}
	return lastErr
}

func transientSnapshotError(err error) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}
	switch postgresError.Code {
	case "3D000", "42P04", "55006":
		return true
	default:
		return false
	}
}

func waitForSnapshotRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

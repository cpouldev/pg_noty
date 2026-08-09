//go:build integration

package testpostgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRestoreSnapshotRetriesWhenTheRestoredDatabaseIsTemporarilyAbsent(t *testing.T) {
	restores, verifications := 0, 0
	err := restoreSnapshot(
		t.Context(),
		func(context.Context) error {
			restores++
			return nil
		},
		func(context.Context) error {
			verifications++
			if verifications == 1 {
				return &pgconn.PgError{Code: "3D000", Message: "database does not exist"}
			}
			return nil
		},
		noRestoreDelay,
	)
	if err != nil || restores != 2 || verifications != 2 {
		t.Fatalf("restore = (%v, %d restores, %d verifications), want nil and two of each", err, restores, verifications)
	}
}

func TestRestoreSnapshotRetriesATransientRestoreFailure(t *testing.T) {
	restores, verifications := 0, 0
	err := restoreSnapshot(
		t.Context(),
		func(context.Context) error {
			restores++
			if restores == 1 {
				return &pgconn.PgError{Code: "42P04", Message: "database already exists"}
			}
			return nil
		},
		func(context.Context) error {
			verifications++
			return nil
		},
		noRestoreDelay,
	)
	if err != nil || restores != 2 || verifications != 1 {
		t.Fatalf("restore = (%v, %d restores, %d verifications), want nil, two restores and one verification", err, restores, verifications)
	}
}

func TestRestoreSnapshotDoesNotRetryAPermanentFailure(t *testing.T) {
	permanent := errors.New("authentication failed")
	restores := 0
	err := restoreSnapshot(
		t.Context(),
		func(context.Context) error {
			restores++
			return permanent
		},
		func(context.Context) error {
			t.Fatal("verified a restore that failed")
			return nil
		},
		noRestoreDelay,
	)
	if !errors.Is(err, permanent) || restores != 1 {
		t.Fatalf("restore = (%v, %d attempts), want the permanent error after one attempt", err, restores)
	}
}

func TestRestoreSnapshotBoundsPersistentTransientFailures(t *testing.T) {
	transient := &pgconn.PgError{Code: "55006", Message: "database is being accessed by other users"}
	restores := 0
	err := restoreSnapshot(
		t.Context(),
		func(context.Context) error {
			restores++
			return transient
		},
		func(context.Context) error {
			t.Fatal("verified a restore that failed")
			return nil
		},
		noRestoreDelay,
	)
	if !errors.Is(err, transient) || restores != 3 {
		t.Fatalf("restore = (%v, %d attempts), want the transient error after three attempts", err, restores)
	}
}

func noRestoreDelay(context.Context, time.Duration) error { return nil }

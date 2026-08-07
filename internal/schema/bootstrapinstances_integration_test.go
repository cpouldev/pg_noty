//go:build integration

package schema

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 21: two instances sharing one database must not serialise on one another.
//
// Both cases are arranged rather than raced, because the claim is a negative one and a negative
// asserted by two things happening to finish is not asserted at all. A key is held by a session of
// its own, and what is measured is whether the boot under test queues behind it -- which the server
// answers, rather than a wall clock.
//
// The second case is what pins the criterion's own reason. Two instances also differ in the schema
// they own, and a key computed from the schema alone would keep *those* apart -- so the row that
// would fail a key ignoring `instance` is the one holding the schema constant and varying only the
// instance.

const (
	theFirstInstance  = "instance_a"
	theSecondInstance = "instance_b"
	// theUnblockedBootDeadline bounds a boot this file expects not to wait at all. It is below
	// DefaultLockTimeout, so a boot that waited out any bound this package can impose fails it.
	theUnblockedBootDeadline = 2 * time.Second
)

// anInstanceConfiguration is one instance's configuration: its own name, and its own schema.
func anInstanceConfiguration(t *testing.T, instance, schemaName string) config.Config {
	t.Helper()

	cfg := aBootConfiguration(t)
	cfg.Instance, cfg.Database.Schema = instance, schemaName
	return cfg
}

// holdTheKey takes one advisory key on a session of its own and answers with its release.
func holdTheKey(t *testing.T, cfg config.Config, key int64) func() {
	t.Helper()

	held, err := acquireLock(t.Context(), acquiredConn(t, anotherReplica(t, cfg)), key)
	if err != nil {
		t.Fatalf("hold key %d on a session of its own: %v", key, err)
	}
	return func() {
		if err := held.release(context.WithoutCancel(t.Context())); err != nil {
			t.Errorf("release key %d: %v", key, err)
		}
	}
}

// TestTwoInstancesSharingADatabaseBothCompleteWithoutWaitingOnEachOther is criterion 21.
//
// The first instance's own key is held throughout, so its boot cannot get past step 3 while the
// second's runs. A second boot that completes anyway cannot have been waiting on the first's lock,
// and both are then seen to finish with their own schema fully created.
func TestTwoInstancesSharingADatabaseBothCompleteWithoutWaitingOnEachOther(t *testing.T) {
	skipIfShort(t)

	pool := freshDatabase(t)
	first := anInstanceConfiguration(t, theFirstInstance, firstTenantSchema)
	second := anInstanceConfiguration(t, theSecondInstance, secondTenantSchema)

	release := holdTheKey(t, first, lockKey(first.Database.Schema, first.Instance))
	blocked := make(chan error, 1)
	go func() { blocked <- Bootstrap(t.Context(), pool, first) }()
	waitUntilReplicasAreQueuedOnTheLock(t, pool, 1)

	bounded, cancel := context.WithTimeout(t.Context(), theUnblockedBootDeadline)
	defer cancel()
	if err := Bootstrap(bounded, pool, second); err != nil {
		t.Fatalf(
			"%s answered %v while %s's key was held; two instances sharing one database must "+
				"not serialise on each other", second.Instance, err, first.Instance,
		)
	}

	release()
	if err := <-blocked; err != nil {
		t.Fatalf("%s answered %v once its own key was free: both configurations complete", first.Instance, err)
	}
	for _, cfg := range []config.Config{first, second} {
		assertTheSchemaIsFullyCreated(t, pool, cfg.Database.Schema)
	}
}

// TestABootWaitsOnItsOwnKeyAndNotOnAnotherInstancesForTheSameSchema is the reason criterion 21
// names: the two rows differ in the instance alone, so a key computed without it makes the accepted
// row block.
func TestABootWaitsOnItsOwnKeyAndNotOnAnotherInstancesForTheSameSchema(t *testing.T) {
	skipIfShort(t)

	for _, tc := range []struct {
		name, heldBy string
		waits        bool
	}{
		{name: "another instance's key on this very schema", heldBy: theForeignInstance},
		{name: "this instance's own key", heldBy: "", waits: true},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				pool := freshDatabase(t)
				cfg := aBootConfiguration(t)

				holder := tc.heldBy
				if holder == "" {
					holder = cfg.Instance
				}
				release := holdTheKey(t, cfg, lockKey(cfg.Database.Schema, holder))

				if !tc.waits {
					assertTheBootDoesNotWait(t, pool, cfg)
					release()
					return
				}
				booted := make(chan error, 1)
				go func() { booted <- Bootstrap(t.Context(), pool, cfg) }()
				waitUntilReplicasAreQueuedOnTheLock(t, pool, 1)
				release()
				if err := <-booted; err != nil {
					t.Fatalf("the boot answered %v once its own key was free: it waits, and then it runs", err)
				}
			},
		)
	}
}

// assertTheBootDoesNotWait runs one boot under a deadline no waiting implementation could meet.
func assertTheBootDoesNotWait(t *testing.T, pool *pgxpool.Pool, cfg config.Config) {
	t.Helper()

	bounded, cancel := context.WithTimeout(t.Context(), theUnblockedBootDeadline)
	defer cancel()

	began := time.Now()
	if err := Bootstrap(bounded, pool, cfg); err != nil {
		t.Fatalf(
			"the boot answered %v after %s while another instance's key on the same schema was "+
				"held; a key computed without the instance would queue exactly like this",
			err, time.Since(began),
		)
	}
}

// assertTheSchemaIsFullyCreated is criterion 21's second half: each instance ends with its own
// schema holding every object this binary declares.
func assertTheSchemaIsFullyCreated(t *testing.T, pool *pgxpool.Pool, schemaName string) {
	t.Helper()

	present := objectNamesIn(t, pool, schemaName)
	for _, declared := range declaredObjectNames() {
		if !slices.Contains(present, declared) {
			t.Errorf("%s is missing from %s, which holds %v", declared, schemaName, present)
		}
	}
}

package reconcile

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

type runLockResult struct {
	held    *runLock
	refusal *Refusal
	err     error
}

func TestRunLockContractHasTheRequiredAuthorities(t *testing.T) {
	source, err := os.ReadFile("runlock.go")
	if err != nil {
		t.Fatalf("read runlock contract: %v", err)
	}
	for _, requirement := range []string{
		"DefaultRunLockWait",
		"60 * time.Second",
		"schema.Reconcile" + "LockKey",
		"pg_try_advisory_lock",
		"func (held *runLock) release(ctx context.Context)",
	} {
		if !strings.Contains(string(source), requirement) {
			t.Errorf("runlock.go does not contain %q", requirement)
		}
	}
}

func TestDefaultRunLockWaitIsTheSpecifiedLiteral(t *testing.T) {
	if DefaultRunLockWait != 60*time.Second {
		t.Errorf("DefaultRunLockWait = %s, want literal %s", DefaultRunLockWait, 60*time.Second)
	}
}

func TestDefaultRunLockWaitIsIndependentOfTableLockTimeout(t *testing.T) {
	if DefaultRunLockWait == 3*time.Second {
		t.Fatalf("run-lock wait %s collapsed onto the 3s table-lock literal", DefaultRunLockWait)
	}
}

func TestRunLockReleaseRequiresTheHandleCarryingThePinnedConnection(t *testing.T) {
	var _ interface{ release(context.Context) error } = (*runLock)(nil)
	if strings.Contains(mustReadRunLock(t), "func Release(") {
		t.Fatal("runlock.go exports a key-only release path")
	}
}

func TestLockProvenanceUsesTheCatalogAuthority(t *testing.T) {
	for path, authority := range map[string]string{"runlock.go": "NewCatalog", "blockerwatch.go": "NewConnectionCatalog"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"pg_" + "locks", "pg_stat_" + "activity", "pg_blocking_" + "pids"} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("%s bypasses Catalog with %q", path, forbidden)
			}
		}
		if !strings.Contains(string(source), authority) {
			t.Fatalf("%s does not consume %s", path, authority)
		}
		if path == "blockerwatch.go" {
			for _, requirement := range []string{"BlockingBackend", "could not be identified", "blockerWatchAcquireBound"} {
				if !strings.Contains(string(source), requirement) {
					t.Errorf("blockerwatch.go does not contain %q", requirement)
				}
			}
		}
	}
}

func mustReadRunLock(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile("runlock.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(source)
}

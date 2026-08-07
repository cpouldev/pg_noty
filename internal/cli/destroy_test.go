package cli

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/reconcile"
)

func TestDestroyGridHasEveryDistinctCell(t *testing.T) {
	seen := map[string]bool{}
	for disagreement := range destroyDisagreementMessages {
		for _, kind := range destroyObjectKinds {
			message := destroyMessage(kind, disagreement, "orders", "foreign")
			if seen[message] {
				t.Fatalf("duplicate grid message %q", message)
			}
			seen[message] = true
			if !strings.Contains(message, kind) || !strings.Contains(message, "orders") {
				t.Fatalf("cell %s/%s omitted identity: %q", disagreement, kind, message)
			}
		}
	}
	if len(seen) != len(destroyObjectKinds)*len(destroyDisagreementMessages) {
		t.Fatalf("grid cells=%d, want product", len(seen))
	}
	if len(destroyDisagreementMessages) != 4 || destroyCellCount() != 8 {
		t.Fatal("ownership vocabulary/grid size changed")
	}
	if reconcile.DisagreementMarkerAbsent == "" {
		t.Fatal("empty disagreement sentinel")
	}
}

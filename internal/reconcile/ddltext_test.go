package reconcile

import (
	"strings"
	"testing"
	"time"
)

func TestDropTextsQuoteCatalogReadNames(t *testing.T) {
	target := TargetReading{Schema: `Odd " Schema`, Table: `Table; --`}
	trigger, err := dropTriggerText(target, `Trigger"; --`)
	if err != nil {
		t.Fatal(err)
	}
	function, err := dropFunctionText(`Service " Schema`, `Function; --`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Trigger""; --"`, `"Odd "" Schema"."Table; --"`, `"Service "" Schema"."Function; --"()`} {
		if !strings.Contains(trigger+function, want) {
			t.Fatalf("drop text %q does not quote %q", trigger+function, want)
		}
	}
}

func TestDropTextsRefuseUnusableCatalogNames(t *testing.T) {
	if _, err := dropTriggerText(TargetReading{Schema: "public", Table: "usable"}, "bad\x00name"); err == nil {
		t.Fatal("drop trigger accepted an unusable catalog name")
	}
	if _, err := dropFunctionText("public", ""); err == nil {
		t.Fatal("drop function accepted an unusable catalog name")
	}
}

func TestApplyLockTimeoutIsThePinnedThreeSecondInteger(t *testing.T) {
	if applyLockTimeout != 3*time.Second {
		t.Fatalf("apply lock timeout = %s, want 3s", applyLockTimeout)
	}
	if got := applyLockTimeoutStatement(applyLockTimeout); got != "SET LOCAL lock_timeout = 3000" {
		t.Fatalf("lock timeout statement = %q, want base-ten integer", got)
	}
	if got := applyLockTimeoutStatement(time.Nanosecond); got != "SET LOCAL lock_timeout = 1" {
		t.Fatalf("sub-millisecond timeout statement = %q, want one millisecond rather than no bound", got)
	}
}

// TestDropTextsCarryNoIfExistsHedge pins the fact objectapply.go and the re-diff rely on: a drop
// rendered for an object that is already gone fails loudly instead of succeeding silently.
func TestDropTextsCarryNoIfExistsHedge(t *testing.T) {
	trigger, err := dropTriggerText(TargetReading{Schema: "public", Table: "orders"}, "noty_orders")
	if err != nil {
		t.Fatal(err)
	}
	function, err := dropFunctionText("noty", "notify_orders")
	if err != nil {
		t.Fatal(err)
	}
	for _, rendered := range []string{trigger, function} {
		if strings.Contains(strings.ToUpper(rendered), "IF "+"EXISTS") {
			t.Fatalf("drop text %q hedges with IF EXISTS, so re-applying a stale plan would report success", rendered)
		}
	}
}

package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

var payloadGridOperations = []string{"insert", "update", "delete"}

var payloadGridModes = []struct {
	name    string
	payload config.Payload
}{
	{"full", config.Payload{Mode: "full"}},
	{"full_exclude", config.Payload{Mode: "full", Exclude: []string{"secret"}}},
	{"columns", config.Payload{Mode: "columns", Columns: []string{"status"}}},
	{"keys_only", config.Payload{Mode: "keys_only"}},
}

func TestPayloadGridCrossesEveryOperationModeAndFlag(t *testing.T) {
	rows := 0
	for _, operation := range payloadGridOperations {
		for _, mode := range payloadGridModes {
			for _, includeOld := range []bool{false, true} {
				rows++
				payload := mode.payload
				payload.IncludeOld = includeOld
				listener := config.Listener{Name: "orders", Trigger: config.TriggerSpec{Payload: payload}}
				got, err := payloadExpressionsFor(operation, listener, Target{PrimaryKeyColumns: []string{"id"}})
				if err != nil || got.new == "" || got.old == "" {
					t.Fatalf("grid cell %s/%s/%t = %#v, err=%v", operation, mode.name, includeOld, got, err)
				}
				assertOperationPayloadCell(t, operation, got)
			}
		}
	}
	if rows != 3*4*2 {
		t.Fatalf("payload grid crossed %d rows, want 24", rows)
	}
}

func assertOperationPayloadCell(t *testing.T, operation string, got payloadExpressions) {
	t.Helper()
	switch operation {
	case "insert":
		if got.old != "NULL::jsonb" {
			t.Errorf("insert old = %q, want NULL::jsonb", got.old)
		}
	case "update":
		if got.old != "NULL::jsonb" && got.old != "to_jsonb(OLD)" {
			t.Errorf("update old = %q", got.old)
		}
	case "delete":
		if got.old != "to_jsonb(OLD)" || got.new != "NULL::jsonb" {
			t.Errorf("delete pair = %#v", got)
		}
	}
}

func TestIncludeOldIsInertForInsertAndDelete(t *testing.T) {
	for _, operation := range []string{"insert", "delete"} {
		without := config.Listener{Name: "n", Trigger: config.TriggerSpec{Payload: config.Payload{Mode: "full"}}}
		with := without
		with.Trigger.Payload.IncludeOld = true
		first, err := payloadExpressionsFor(operation, without, Target{})
		if err != nil {
			t.Fatal(err)
		}
		second, err := payloadExpressionsFor(operation, with, Target{})
		if err != nil {
			t.Fatal(err)
		}
		if first != second {
			t.Errorf("%s include_old changed %#v to %#v", operation, first, second)
		}
	}
}

func TestIncludeOldIsActiveOnlyForUpdate(t *testing.T) {
	without := config.Listener{Name: "n", Trigger: config.TriggerSpec{Payload: config.Payload{Mode: "full"}}}
	with := without
	with.Trigger.Payload.IncludeOld = true
	first, err := payloadExpressionsFor("update", without, Target{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := payloadExpressionsFor("update", with, Target{})
	if err != nil {
		t.Fatal(err)
	}
	if first.old != "NULL::jsonb" || second.old != "to_jsonb(OLD)" || first == second {
		t.Fatalf("update include_old pairs = %#v and %#v", first, second)
	}
	if strings.Contains(first.new, "OLD") || !strings.Contains(second.old, "OLD") {
		t.Fatalf("update old expressions are not scoped correctly: %#v %#v", first, second)
	}
}

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
	{"columns", config.Payload{Mode: "columns", Columns: []string{"id", "status"}}},
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
				assertOperationPayloadCell(t, operation, payload, got)
			}
		}
	}
	if rows != 3*4*2 {
		t.Fatalf("payload grid crossed %d rows, want 24", rows)
	}
}

func assertOperationPayloadCell(t *testing.T, operation string, payload config.Payload, got payloadExpressions) {
	t.Helper()
	wantOld := "NULL::jsonb"
	if operation == "delete" || operation == "update" && payload.IncludeOld {
		wantOld = "to_jsonb(OLD)"
		if payload.Mode == "columns" {
			// The configured order is id then status, so the old object must contain exactly
			// those two keys in that order and no whole-row conversion.
			wantOld = `jsonb_build_object('id', OLD."id", 'status', OLD."status")`
		}
	}
	if got.old != wantOld {
		t.Errorf("%s/%s/include_old=%t old = %q, want %q", operation, payload.Mode, payload.IncludeOld, got.old, wantOld)
	}
	if operation == "delete" && got.new != "NULL::jsonb" {
		t.Errorf("delete new = %q, want NULL::jsonb", got.new)
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

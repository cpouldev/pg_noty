package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

var payloadGridOperations = []string{"insert", "update", "delete"}

// wantOld is the expression the mode ships for the old row when the old row is active,
// written out from the configuration rather than from a run: keys go through quoteLiteral
// and columns through schema.Quoted, joined by ", ". Every mode is a filter, so each of
// these carries the same filter the mode applies to the new row -- a whole-row conversion
// here for any mode but "full" with no exclude list would ship a withheld column.
type payloadGridMode struct {
	name    string
	payload config.Payload
	wantOld string
}

var payloadGridModes = []payloadGridMode{
	{"full", config.Payload{Mode: "full"}, `to_jsonb(OLD)`},
	{"full_exclude", config.Payload{Mode: "full", Exclude: []string{"secret"}}, `to_jsonb(OLD) - 'secret'`},
	{"columns", config.Payload{Mode: "columns", Columns: []string{"id", "status"}},
		`jsonb_build_object('id', OLD."id", 'status', OLD."status")`},
	{"keys_only", config.Payload{Mode: "keys_only"}, `jsonb_build_object('id', OLD."id")`},
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
				assertOperationPayloadCell(t, operation, mode, includeOld, got)
			}
		}
	}
	if rows != 3*4*2 {
		t.Fatalf("payload grid crossed %d rows, want 24", rows)
	}
}

func assertOperationPayloadCell(t *testing.T, operation string, mode payloadGridMode, includeOld bool, got payloadExpressions) {
	t.Helper()
	wantOld := "NULL::jsonb"
	if operation == "delete" || operation == "update" && includeOld {
		wantOld = mode.wantOld
	}
	if got.old != wantOld {
		t.Errorf("%s/%s/include_old=%t old = %q, want %q", operation, mode.name, includeOld, got.old, wantOld)
	}
	if operation == "delete" && got.new != "NULL::jsonb" {
		t.Errorf("delete new = %q, want NULL::jsonb", got.new)
	}
}

// TestEveryModeFiltersTheOldRowAsItFiltersTheNewRow states the class the grid rows are
// instances of. A payload mode is a filter over a row, so a mode routed for NEW and left as a
// whole-row conversion for OLD delivers, under data.old, exactly the columns the operator
// configured the mode to withhold.
func TestEveryModeFiltersTheOldRowAsItFiltersTheNewRow(t *testing.T) {
	for _, mode := range payloadGridModes {
		payload := mode.payload
		payload.IncludeOld = true
		listener := config.Listener{Name: "orders", Trigger: config.TriggerSpec{Payload: payload}}
		got, err := payloadExpressionsFor("update", listener, Target{PrimaryKeyColumns: []string{"id"}})
		if err != nil {
			t.Fatalf("%s: %v", mode.name, err)
		}
		if want := strings.ReplaceAll(got.new, "NEW", "OLD"); got.old != want {
			t.Errorf("%s old = %q, want %q: the old row carries the filter the new row carries",
				mode.name, got.old, want)
		}
	}
}

// TestThePayloadGridCoversEveryDeclaredMode keeps the grid closed over the mode set rather
// than over the rows someone remembered, so a fourth mode arrives with a row asserting what it
// ships for the old row instead of inheriting another mode's.
func TestThePayloadGridCoversEveryDeclaredMode(t *testing.T) {
	covered := map[string]struct{}{}
	for _, mode := range payloadGridModes {
		covered[mode.payload.Mode] = struct{}{}
	}
	for _, declared := range payloadModes {
		if _, found := covered[declared]; !found {
			t.Errorf("payload mode %q is declared and has no grid row", declared)
		}
	}
	if len(covered) != len(payloadModes) {
		t.Fatalf("the grid covers %d modes, want the declared %d", len(covered), len(payloadModes))
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

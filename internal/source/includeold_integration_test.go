//go:build integration

package source

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

type includeOldModeCase struct {
	name           string
	payload        config.Payload
	wantOldColumns []string
}

var includeOldModeCases = []includeOldModeCase{
	{name: "full", payload: config.Payload{Mode: "full"}, wantOldColumns: []string{"id", "status", "secret"}},
	{
		name: "columns", payload: config.Payload{Mode: "columns", Columns: []string{"id", "status"}},
		wantOldColumns: []string{"id", "status"},
	},
}

func TestIncludeOldGridAcrossOperations(t *testing.T) {
	skipIfShort(t)
	for _, mode := range includeOldModeCases {
		for _, operation := range []string{"insert", "update", "delete"} {
			for _, includeOld := range []bool{false, true} {
				t.Run(mode.name+"/"+operation+"/old="+boolText(includeOld), func(t *testing.T) {
					testIncludeOldCell(t, mode, operation, includeOld)
				})
			}
		}
	}
}

func testIncludeOldCell(t *testing.T, mode includeOldModeCase, operation string, includeOld bool) {
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.old_target (id int PRIMARY KEY, status text, secret text)`)
	request := generationRequest(config.Operation{Kind: operation})
	request.Target.Table = "old_target"
	request.Listener.Trigger.Payload = mode.payload
	request.Listener.Trigger.Payload.IncludeOld = includeOld
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])
	mustExecOn(t, pool, `INSERT INTO public.old_target VALUES (1, 'before', 'not-selected-in-columns-mode')`)
	if operation == "update" {
		mustExecOn(t, pool, `UPDATE public.old_target SET status='after' WHERE id=1`)
	} else if operation == "delete" {
		mustExecOn(t, pool, `DELETE FROM public.old_target WHERE id=1`)
	}
	payload := eventPayload(t, pool)
	wantOld := operation == "delete" || operation == "update" && includeOld
	if !wantOld {
		if payload["old"] != nil {
			t.Fatalf("operation %s old = %#v, want null", operation, payload["old"])
		}
		return
	}
	old, ok := payload["old"].(map[string]any)
	if !ok {
		t.Fatalf("operation %s old has wrong shape: %#v", operation, payload["old"])
	}
	if len(old) != len(mode.wantOldColumns) {
		t.Fatalf("operation %s old has %d fields %#v, want %d", operation, len(old), old, len(mode.wantOldColumns))
	}
	for _, column := range mode.wantOldColumns {
		if _, ok := old[column]; !ok {
			t.Errorf("operation %s old omitted column %q: %#v", operation, column, old)
		}
	}
	if mode.payload.Mode == "columns" {
		if _, exposed := old["secret"]; exposed {
			t.Errorf("operation %s old exposed an unconfigured column: %#v", operation, old)
		}
	}
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

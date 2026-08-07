//go:build integration

package source

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func TestIncludeOldGridAcrossOperations(t *testing.T) {
	skipIfShort(t)
	for _, operation := range []string{"insert", "update", "delete"} {
		for _, includeOld := range []bool{false, true} {
			t.Run(
				operation+"/old="+boolText(includeOld), func(t *testing.T) {
					pool := freshDatabase(t)
					applySourceMigrations(t, pool)
					mustExecOn(t, pool, `CREATE TABLE public.old_target (id int PRIMARY KEY, status text)`)
					request := generationRequest(config.Operation{Kind: operation})
					request.Target.Table = "old_target"
					request.Listener.Trigger.Payload = config.Payload{Mode: "full", IncludeOld: includeOld}
					sets, err := Generate(request)
					if err != nil {
						t.Fatal(err)
					}
					executeObjectSet(t, pool, sets[0])
					mustExecOn(t, pool, `INSERT INTO public.old_target VALUES (1, 'before')`)
					if operation == "update" {
						mustExecOn(t, pool, `UPDATE public.old_target SET status='after' WHERE id=1`)
					} else if operation == "delete" {
						mustExecOn(t, pool, `DELETE FROM public.old_target WHERE id=1`)
					}
					payload := eventPayload(t, pool)
					if operation == "update" && includeOld {
						if payload["old"] == nil {
							t.Fatal("update include_old omitted old")
						}
					} else if operation == "delete" {
						if payload["old"] == nil {
							t.Fatal("delete omitted old")
						}
					} else if payload["old"] != nil {
						t.Fatalf("operation %s old = %#v, want null", operation, payload["old"])
					}
				},
			)
		}
	}
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

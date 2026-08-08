package source

import (
	"reflect"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func TestIsDistinctBuildsDeterministicUpdateConditions(t *testing.T) {
	tests := []struct {
		name      string
		operation config.Operation
		want      string
	}{
		{
			name: "selected hostile and mixed-case columns",
			operation: config.Operation{
				Kind: "update", Columns: []string{"Status", `a"b`}, IsDistinct: true,
			},
			want: `CREATE TRIGGER "fn" AFTER UPDATE ON "public"."orders" FOR EACH ROW WHEN (` +
				`pg_catalog.to_jsonb(ROW(OLD."Status", OLD."a""b")) IS DISTINCT FROM ` +
				`pg_catalog.to_jsonb(ROW(NEW."Status", NEW."a""b"))) ` +
				`EXECUTE FUNCTION "noty"."fn"();`,
		},
		{
			name:      "whole row",
			operation: config.Operation{Kind: "update", IsDistinct: true},
			want: `CREATE TRIGGER "fn" AFTER UPDATE ON "public"."orders" FOR EACH ROW WHEN (` +
				`pg_catalog.to_jsonb(OLD) IS DISTINCT FROM pg_catalog.to_jsonb(NEW)) ` +
				`EXECUTE FUNCTION "noty"."fn"();`,
		},
		{
			name: "explicit when",
			operation: config.Operation{
				Kind: "update", Columns: []string{"status"}, IsDistinct: true,
				When: "NEW.enabled OR NEW.priority > 10",
			},
			want: `CREATE TRIGGER "fn" AFTER UPDATE ON "public"."orders" FOR EACH ROW WHEN ((` +
				`pg_catalog.to_jsonb(ROW(OLD."status")) IS DISTINCT FROM ` +
				`pg_catalog.to_jsonb(ROW(NEW."status"))) AND (NEW.enabled OR NEW.priority > 10)) ` +
				`EXECUTE FUNCTION "noty"."fn"();`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			statements, err := statementsFor(statementRequest("orders"), tc.operation, "fn")
			if err != nil {
				t.Fatal(err)
			}
			if statements.createTrigger != tc.want {
				t.Fatalf("create trigger =\n%s\nwant\n%s", statements.createTrigger, tc.want)
			}
		})
	}
}

func TestExplicitFalsePreservesTheExistingDDL(t *testing.T) {
	omitted, err := statementsFor(statementRequest("orders"),
		config.Operation{Kind: "update", Columns: []string{"status"}}, "fn")
	if err != nil {
		t.Fatal(err)
	}
	explicitFalse, err := statementsFor(statementRequest("orders"),
		config.Operation{Kind: "update", Columns: []string{"status"}, IsDistinct: false}, "fn")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(explicitFalse, omitted) {
		t.Fatalf("is_distinct=false DDL differs from omitted:\nfalse   %+v\nomitted %+v", explicitFalse, omitted)
	}
}

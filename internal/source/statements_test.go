package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
)

func statementRequest(table string) Request {
	return Request{
		Instance: "prod", ServiceSchema: "noty", Listener: config.Listener{
			Name: "order_paid", Trigger: config.TriggerSpec{Payload: config.Payload{Mode: "full"}},
		}, Target: Target{Schema: "public", Table: table, PrimaryKeyColumns: []string{"id"}},
	}
}

func TestStatementsCarryThePrivilegeTriadAndBothComments(t *testing.T) {
	statements, err := statementsFor(
		statementRequest("orders"),
		config.Operation{Kind: "update", Columns: []string{"status"}, When: "NEW.status IS NOT NULL"},
		"pg_noty_order_paid_upd",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"LANGUAGE plpgsql", "SECURITY DEFINER", "SET search_path = pg_catalog, pg_temp", "RETURN NULL",
		"REVOKE EXECUTE", "FROM PUBLIC", "UPDATE OF \"status\"", "WHEN (NEW.status IS NOT NULL)",
	} {
		if !strings.Contains(statements.createFunction+statements.revokeExecute+statements.createTrigger, want) {
			t.Errorf("statements lack %q", want)
		}
	}
	for _, statement := range []string{statements.commentFunction, statements.commentTrigger} {
		if !strings.Contains(statement, "pg_noty:v1:prod:order_paid:update") {
			t.Errorf("comment %q lacks long marker", statement)
		}
	}
}

func TestUpdateOfIsScopedToUpdate(t *testing.T) {
	for _, operation := range []string{"insert", "delete"} {
		statements, err := statementsFor(
			statementRequest("orders"),
			config.Operation{Kind: operation, Columns: []string{"status"}},
			"fn",
		)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(statements.createTrigger, "UPDATE OF") {
			t.Errorf("%s trigger has an UPDATE OF filter", operation)
		}
	}
}

// TestDollarTagSeesTheRenderedTableName asserts which tag the generator picks, which is a claim
// about our arithmetic. Where the function body actually ends is PostgreSQL's lexer's decision, and
// that half is asserted against a real server by
// TestADollarQuoteBearingNameIsCreatedAndInertOnTheServer (dollarexec_integration_test.go): a
// closing delimiter that drifted from the chosen tag still satisfies the match below.
func TestDollarTagSeesTheRenderedTableName(t *testing.T) {
	for _, tc := range []struct{ table, tag string }{{"a$fn$", "$fn_1$"}, {"a$fn$$fn_1$", "$fn_2$"}} {
		statements, err := statementsFor(statementRequest(tc.table), config.Operation{Kind: "insert"}, "fn")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(statements.createFunction, "AS "+tc.tag) {
			t.Errorf("table %q did not select %s", tc.table, tc.tag)
		}
	}
}

func TestStoredTableNameUsesQualifiedLiteralAndRoundTrips(t *testing.T) {
	for _, tc := range []struct{ schema, table string }{{"public", "orders"}, {"a.b", `c.d`}, {"a$b", `c"d`}} {
		request := statementRequest(tc.table)
		request.Target.Schema = tc.schema
		body, err := triggerBody(request, config.Operation{Kind: "insert"})
		if err != nil {
			t.Fatal(err)
		}
		qualified, fault := schema.Qualified(tc.schema, tc.table)
		if fault != schema.IdentifierOK || !strings.Contains(body, quoteLiteral(qualified)) {
			t.Fatalf("body %q lacks qualified stored table %q", body, qualified)
		}
		gotSchema, gotTable, ok := SplitQualified(qualified)
		if !ok || gotSchema != tc.schema || gotTable != tc.table {
			t.Errorf("SplitQualified(%q) = %q,%q,%t", qualified, gotSchema, gotTable, ok)
		}
	}
}

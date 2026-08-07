//go:build integration

package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The dollar-quote tag is the one place in this package where PostgreSQL's lexer, and not our Go,
// decides where the generated text ends: a body holding the tag it is wrapped in closes the
// function early and exposes the rest as code. statements_test.go asserts which tag the generator
// picks by string match, which is a claim about our arithmetic rather than about the lexer. These
// cases put the tag-bearing name in front of a real server.
var theDollarBearingTables = []struct{ table, tag string }{
	{`a$fn$`, "$fn_1$"},
	{`a$fn$$fn_1$`, "$fn_2$"},
}

const storedFunctionBodyQuery = `
SELECT p.prosrc
  FROM pg_catalog.pg_proc p
  JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
 WHERE n.nspname = 'noty' AND p.proname = $1`

// TestADollarQuoteBearingNameIsCreatedAndInertOnTheServer creates the function, then reads the body
// PostgreSQL stored for it. prosrc is exactly the text between the chosen delimiters, so comparing
// it with what the generator rendered is the lexer's own answer to "where does this function end":
// a tag that closed early cannot be created at all, and one that swallowed more than its body
// stores something else.
func TestADollarQuoteBearingNameIsCreatedAndInertOnTheServer(t *testing.T) {
	skipIfShort(t)
	for _, testCase := range theDollarBearingTables {
		t.Run(
			testCase.tag, func(t *testing.T) {
				pool := freshDatabase(t)
				applySourceMigrations(t, pool)
				quoted, fault := schema.Quoted(testCase.table)
				if fault != schema.IdentifierOK {
					t.Fatalf("table %q is unusable: %s", testCase.table, fault)
				}
				mustExecOn(t, pool, "CREATE TABLE public."+quoted+" (id int)")

				request := generationRequest(config.Operation{Kind: "insert"})
				request.Target.Table = testCase.table
				sets := theDollarBearingSet(t, request, testCase.tag)
				executeObjectSet(t, pool, sets)

				assertStoredBodyIsTheRenderedBody(t, pool, request, sets.FunctionName)
				assertTheRowLandsUnderTheDollarName(t, pool, quoted, testCase.table)
			},
		)
	}
}

// theDollarBearingSet also asserts the precondition this case declares: if the generator stopped
// choosing the ascended tag, the case would still create a function and would no longer be about a
// collision.
func theDollarBearingSet(t *testing.T, request Request, tag string) ObjectSet {
	t.Helper()
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sets[0].CreateFunction, "AS "+tag) {
		t.Fatalf(
			"the generator chose a tag other than %s, so this case no longer covers the "+
				"collision it was written for:\n%s", tag, sets[0].CreateFunction,
		)
	}
	return sets[0]
}

func assertStoredBodyIsTheRenderedBody(t *testing.T, pool *pgxpool.Pool, request Request, function string) {
	t.Helper()
	body, err := triggerBody(request, config.Operation{Kind: "insert"})
	if err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := pool.QueryRow(t.Context(), storedFunctionBodyQuery, function).Scan(&stored); err != nil {
		t.Fatalf("read the stored body of noty.%s back: %v", function, err)
	}
	// statements.go writes AS $tag$\n<body>\n$tag$, so the text between the delimiters is the body
	// with one newline on each side.
	if want := "\n" + body + "\n"; stored != want {
		t.Errorf(
			"the server stored %d bytes between the delimiters and the generator rendered "+
				"%d; the chosen tag does not bound the text it was chosen for.\nstored:\n%s",
			len(stored), len(want), stored,
		)
	}
}

func assertTheRowLandsUnderTheDollarName(t *testing.T, pool *pgxpool.Pool, quoted, table string) {
	t.Helper()
	mustExecOn(t, pool, "INSERT INTO public."+quoted+" (id) VALUES (1)")

	var stored string
	if err := pool.QueryRow(
		t.Context(),
		"SELECT table_name FROM noty.events ORDER BY id DESC LIMIT 1",
	).Scan(&stored); err != nil {
		t.Fatalf("read the event the dollar-bearing table produced: %v", err)
	}
	gotSchema, gotTable, split := SplitQualified(stored)
	if !split || gotSchema != "public" || gotTable != table {
		t.Errorf(
			"the event names %q, which does not round-trip the dollar-bearing table %q",
			stored, table,
		)
	}
}

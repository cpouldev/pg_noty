//go:build integration

package schema

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	theTriggeredTarget     = "public.orders"
	theEveryDatabaseSchema = "public"
	theRouteListener       = "orders"
)

type privilegeFixture struct {
	privileged  *pgxpool.Pool
	service     *pgxpool.Pool
	application *pgxpool.Pool
}

func theTargetSchemas(t *testing.T) []string {
	t.Helper()

	needed := []string{harnessSchema}
	for _, target := range fixtureTargets {
		schema, _, isQualified := strings.Cut(target, targetSeparator)
		if !isQualified {
			t.Fatalf("the configured target %q names no schema, so the grant set emitted for it "+
				"could not be applied here either", target)
		}
		if schema != theEveryDatabaseSchema && !slices.Contains(needed, schema) {
			needed = append(needed, schema)
		}
	}
	return needed
}

func createTheTargetTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	for _, target := range fixtureTargets {
		schema, table, _ := strings.Cut(target, targetSeparator)
		mustExecOn(t, pool, "CREATE TABLE "+mustQualify(t, schema, table)+
			" (id bigint PRIMARY KEY, note text)")
	}
	if !slices.Contains(fixtureTargets, theTriggeredTarget) {
		t.Fatalf("the route runs through %s, which is not one of the configured targets %q",
			theTriggeredTarget, fixtureTargets)
	}
}

func applyGrantFamilies(t *testing.T, pool *pgxpool.Pool, statements []string, want int,
	families ...grantFamily) {
	t.Helper()

	applied := 0
	for _, statement := range statements {
		if !slices.Contains(families, familyOf(statement)) {
			continue
		}
		mustExecOn(t, pool, statement)
		applied++
	}
	if applied != want {
		t.Fatalf("%d of the %d emitted statements are in %v, want %d",
			applied, len(statements), families, want)
	}
}

type generatedRoute struct {
	createFunction, revokeExecute, commentFunction, createTrigger, commentTrigger string
	qualifiedFunction                                                             string
}

func generatedInsertRoute(t *testing.T) generatedRoute {
	paths, err := filepath.Glob("../source/testdata/*.golden")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		sections := sourceGoldenSections(string(data))
		route := generatedRoute{createFunction: sections["CreateFunction"], revokeExecute: sections["RevokeExecute"], commentFunction: sections["CommentFunction"], createTrigger: sections["CreateTrigger"], commentTrigger: sections["CommentTrigger"]}
		if strings.Contains(route.createFunction, "to_jsonb(NEW)") && strings.Contains(route.createTrigger, `AFTER INSERT ON "public"."orders"`) {
			route.qualifiedFunction = qualifiedFunctionName(t, route.createFunction)
			return route
		}
	}
	t.Fatal("committed source golden corpus has no generated INSERT route for public.orders")
	return generatedRoute{}
}

func sourceGoldenSections(data string) map[string]string {
	sections := map[string]string{}
	section := ""
	var body strings.Builder
	flush := func() {
		if section != "" {
			sections[section] = strings.TrimSpace(body.String())
		}
	}
	for _, line := range strings.Split(data, "\n") {
		if sourceGoldenHeader(line) {
			flush()
			section, body = line, strings.Builder{}
			continue
		}
		if section != "" {
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}
	flush()
	return sections
}

func sourceGoldenHeader(line string) bool {
	switch line {
	case "CreateFunction", "RevokeExecute", "CommentFunction", "CreateTrigger", "CommentTrigger":
		return true
	default:
		return false
	}
}

func qualifiedFunctionName(t *testing.T, statement string) string {
	const prefix = "CREATE OR REPLACE FUNCTION "
	if !strings.HasPrefix(statement, prefix) {
		t.Fatalf("generated function section has unexpected prefix: %q", statement)
	}
	name := statement[len(prefix):]
	end := strings.Index(name, "()")
	if end < 1 {
		t.Fatalf("generated function section has no callable name: %q", statement)
	}
	return name[:end]
}

func aPrivilegeFixture(t *testing.T) privilegeFixture {
	privileged := emptySchemas(t, theTargetSchemas(t)...)
	mustCreateRole(t, privileged, theServiceRole, theServicePassword)
	mustCreateRole(t, privileged, theApplicationRole, theApplicationPassword)
	createTheTargetTables(t, privileged)

	emitted := theAppliedGrants(t)
	applyGrantFamilies(t, privileged, emitted, 3, familyOwnership, familyCreate, familyUsage)

	service := poolAs(t, theServiceRole, theServicePassword)
	mustMigrate(t, service, harnessSchema, embeddedCorpusOrFail(t))
	route := generatedInsertRoute(t)
	for _, statement := range []string{route.createFunction, route.revokeExecute, route.commentFunction} {
		mustExecOn(t, service, statement)
	}

	assertTheTriggerGrantIsNecessary(t, service)
	applyGrantFamilies(t, privileged, emitted, len(fixtureTargets), familyTrigger)
	mustExecOn(t, service, route.createTrigger)
	mustExecOn(t, privileged, route.commentTrigger)

	schema, table, _ := strings.Cut(theTriggeredTarget, targetSeparator)
	mustExecOn(t, privileged, "GRANT SELECT, INSERT ON TABLE "+mustQualify(t, schema, table)+
		" TO "+mustQuote(t, theApplicationRole))

	return privilegeFixture{privileged: privileged, service: service,
		application: poolAs(t, theApplicationRole, theApplicationPassword)}
}

func assertTheTriggerGrantIsNecessary(t *testing.T, service *pgxpool.Pool) {
	t.Helper()

	_, refused := service.Exec(t.Context(), generatedInsertRoute(t).createTrigger)
	_, table, _ := strings.Cut(theTriggeredTarget, targetSeparator)

	var fromServer *pgconn.PgError
	if !errors.As(refused, &fromServer) {
		t.Fatalf("%s created a trigger on %s without the TRIGGER grant (%v), so the grant that "+
			"family exists for is not necessary and the set is wider than it needs to be",
			theServiceRole, theTriggeredTarget, refused)
	}
	if fromServer.Code != insufficientPrivilege {
		t.Errorf("the refusal carries SQLSTATE %s (%s), want %s",
			fromServer.Code, fromServer.Message, insufficientPrivilege)
	}
	if !namesAsAWord(fromServer.Message, table) {
		t.Errorf("the refusal %q does not name the table the grant is needed on", fromServer.Message)
	}
}

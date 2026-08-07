//go:build integration

package schema

import (
	"errors"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// Criterion 42 in both of its directions, which fail differently and are therefore asserted apart.
// The security half -- refused on a read *and* on a write -- is what shows the service schema is
// not accidentally open; a read-only check would pass a schema granted SELECT. The functional half
// is what shows the route works at all, and it is a property of the ownership and grants *this*
// package establishes: a schema created under the wrong owner fails here rather than as a
// customer-wide `permission denied` in internal/source.

const (
	// theRouteMarker is written by the application role into the target table and looked for in the
	// event log, so the landing check cannot be satisfied by a row some other case wrote.
	theRouteMarker  = "written by the application role through the definer route"
	theRouteOrderID = 7
)

// TestTheApplicationRoleNeedsNoPrivilegeOnTheServiceSchema is criterion 42. The three cases are
// subtests of one fixture because they are three readings of one privilege state, and building that
// state three times would let them drift apart.
func TestTheApplicationRoleNeedsNoPrivilegeOnTheServiceSchema(t *testing.T) {
	skipIfShort(t)

	fixture := aPrivilegeFixture(t)
	events := mustQualify(t, harnessSchema, "events")

	t.Run("criterion 42, security: a direct read of the event log is refused", func(t *testing.T) {
		assertRefusedNamingTheSchema(t, fixture, "SELECT count(*) FROM "+events)
	})

	t.Run("criterion 42, security: a direct write to the event log is refused", func(t *testing.T) {
		assertRefusedNamingTheSchema(t, fixture, theDirectWrite(events))
	})

	t.Run("criterion 42, functional: the target-table write lands an event", func(t *testing.T) {
		assertGeneratedFunctionIsOwnedBySchemaOwner(t, fixture)
		assertGeneratedRouteLandsTheEvent(t, fixture)
	})
}

// theDirectWrite is the security half's second statement: an event written straight into the log,
// which is exactly what the definer route does on the application role's behalf. Its payload carries
// no note, so the control run of it cannot be mistaken for the routed event below.
func theDirectWrite(events string) string {
	return "INSERT INTO " + events + " (listener, table_name, operation, payload, txid, occurred_at)" +
		" VALUES ('" + theRouteListener + "', '" + theTriggeredTarget + "', 'INSERT', '{}'::jsonb," +
		" pg_current_xact_id(), now())"
}

// assertRefusedNamingTheSchema has three clauses because a refusal can fail in three ways: not a
// server error at all, a server error that is not a *permission* error -- a missing object refuses
// too, and would read as success here -- and a permission error that does not tell the operator
// which schema is closed.
//
// The last line is the control, and it is why the second clause can be trusted: the same statement,
// run by a role that may run it, has to succeed. Without it a typo would be indistinguishable from a
// role that was refused.
func assertRefusedNamingTheSchema(t *testing.T, fixture privilegeFixture, statement string) {
	t.Helper()

	_, refused := fixture.application.Exec(t.Context(), statement)

	var fromServer *pgconn.PgError
	if !errors.As(refused, &fromServer) {
		t.Fatalf("%s ran `%s` and was answered %v, want the server's refusal: the service schema is "+
			"open to a role holding no privilege on it", theApplicationRole, statement, refused)
	}
	if fromServer.Code != insufficientPrivilege {
		t.Errorf("`%s` was refused with SQLSTATE %s (%s), want %s: only a permission error is a "+
			"refusal, and a missing object would otherwise read as one",
			statement, fromServer.Code, fromServer.Message, insufficientPrivilege)
	}
	if !namesAsAWord(fromServer.Message, harnessSchema) {
		t.Errorf("the refusal %q does not name the schema %q it closed",
			fromServer.Message, harnessSchema)
	}
	mustExecOn(t, fixture.privileged, statement)
}

// namesAsAWord reports whether a server message names an object rather than merely spelling it
// somewhere. A substring test is not enough here: the harness database is notyharness and the schema
// is noty, so `permission denied for database notyharness` would satisfy one -- and a database-level
// refusal is a different refusal from the one criterion 42 asks for.
func namesAsAWord(message, name string) bool {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(message)
}

// theDefinerQuery reads the two properties the route depends on and that nothing else in this
// package would notice were wrong: the function is SECURITY DEFINER, and the role it runs as is the
// schema's owner.
const theDefinerQuery = `
SELECT p.prosecdef, definer.rolname, owner.rolname
  FROM pg_catalog.pg_proc p
  JOIN pg_catalog.pg_roles definer ON definer.oid = p.proowner
  JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
  JOIN pg_catalog.pg_roles owner ON owner.oid = n.nspowner
 WHERE p.oid = to_regprocedure($1)`

func assertGeneratedFunctionIsOwnedBySchemaOwner(t *testing.T, fixture privilegeFixture) {
	t.Helper()

	route := generatedInsertRoute(t)
	var definer bool
	var runsAs, ownsTheSchema string
	if err := fixture.privileged.QueryRow(t.Context(), theDefinerQuery,
		route.qualifiedFunction+"()").
		Scan(&definer, &runsAs, &ownsTheSchema); err != nil {
		t.Fatalf("read generated function %s back from the catalog: %v", route.qualifiedFunction, err)
	}

	if !definer {
		t.Errorf("%s is not SECURITY DEFINER, so the application role may be writing the event log itself", route.qualifiedFunction)
	}
	if runsAs != ownsTheSchema {
		t.Errorf("%s runs as %s and %s is owned by %s", route.qualifiedFunction, runsAs, harnessSchema, ownsTheSchema)
	}
	if runsAs != theServiceRole {
		t.Errorf("%s runs as %s, want the documented owner %s", route.qualifiedFunction, runsAs, theServiceRole)
	}
}

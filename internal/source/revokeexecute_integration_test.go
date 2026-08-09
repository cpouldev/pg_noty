//go:build integration

package source

import (
	"errors"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The REVOKE EXECUTE claim, both directions. Nothing called a generated function directly:
// TestGeneratedPrivilegeShapeComesFromTheCatalog reads pg_proc and
// TestLowPrivilegeWriterCanFireButCannotTouchServiceSchema is about the service schema and never
// touches EXECUTE. A generator that emitted no REVOKE at all passed both.
//
// The writing role is given USAGE on the service schema and INSERT on its own table, so EXECUTE is
// the only privilege it lacks and the refusal below cannot be a schema refusal wearing the same
// SQLSTATE. The grant at the end is the other side of the guard: with EXECUTE held, the same call
// is refused for a different reason, which is what shows the first refusal was about EXECUTE and
// not about the shape of the call.

const (
	// insufficientPrivilege is PostgreSQL's SQLSTATE for a privilege refusal, class 42, code 501.
	insufficientPrivilege = "42501"
	theRevokeRole         = "revoke_writer"
	theRevokePassword     = "revoke-password"
	theRevokeTable        = "revoke_target"
)

// sqlStateOf is the server's own code for a refusal, or the empty string when the call did not
// fail. Reading the code rather than the message is what keeps "refused" from meaning "errored".
func sqlStateOf(err error) string {
	var refusal *pgconn.PgError
	if errors.As(err, &refusal) {
		return refusal.Code
	}
	return ""
}

func callFunctionDirectly(t *testing.T, pool *pgxpool.Pool, functionName string) error {
	t.Helper()
	qualified, fault := schema.Qualified(harnessSchema, functionName)
	if fault != schema.IdentifierOK {
		t.Fatalf("qualify %s: %s", functionName, fault)
	}
	_, err := pool.Exec(t.Context(), "SELECT "+qualified+"()")
	return err
}

func TestRevokingExecuteRefusesADirectCallAndStillLetsTheTriggerFire(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.`+theRevokeTable+` (id int)`)
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Target.Table = theRevokeTable
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])

	createTheLimitedRole(t, pool, theRevokeRole, theRevokePassword)
	mustExecOn(t, pool, `GRANT INSERT ON public.`+theRevokeTable+` TO `+theRevokeRole)
	mustExecOn(t, pool, `GRANT USAGE ON SCHEMA noty TO `+theRevokeRole)
	writer := openPoolAs(t, asRole(t, harnessConfig(t), theRevokeRole, theRevokePassword), theRevokeRole)

	// First direction: the role holds no EXECUTE, and the direct call is refused for that.
	refused := callFunctionDirectly(t, writer, sets[0].FunctionName)
	if got := sqlStateOf(refused); got != insufficientPrivilege {
		t.Fatalf(
			"calling %s directly returned SQLSTATE %s (%v), want %s: PUBLIC keeps EXECUTE on a "+
				"new function, so this is the REVOKE the generator emits", sets[0].FunctionName, got,
			refused, insufficientPrivilege,
		)
	}

	// Second direction: the same role's write still fires the trigger, and the event lands.
	assertTheTriggerStillFires(t, pool, writer)

	// The other side of the guard: with EXECUTE granted, the call is still refused -- a trigger
	// function cannot be called as an ordinary one -- but no longer for want of privilege.
	mustExecOn(t, pool, `GRANT EXECUTE ON FUNCTION noty.`+sets[0].FunctionName+`() TO `+theRevokeRole)
	granted := callFunctionDirectly(t, writer, sets[0].FunctionName)
	if granted == nil {
		t.Fatal(
			"the direct call succeeded once EXECUTE was granted, so a trigger function is being " +
				"run as an ordinary one",
		)
	}
	if got := sqlStateOf(granted); got == insufficientPrivilege {
		t.Fatalf(
			"the direct call still returns %s with EXECUTE granted, so the refusal above was "+
				"about some other privilege and the REVOKE claim's first direction is unasserted", got,
		)
	}
}

func assertTheTriggerStillFires(t *testing.T, pool, writer *pgxpool.Pool) {
	t.Helper()
	if _, err := writer.Exec(t.Context(), `INSERT INTO public.`+theRevokeTable+` VALUES (1)`); err != nil {
		t.Fatalf("the role holding no EXECUTE could not write to its own table: %v", err)
	}
	var events, queued int
	if err := pool.QueryRow(
		t.Context(),
		"SELECT count(*) FROM noty.events WHERE table_name='\"public\".\""+theRevokeTable+"\"'",
	).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM noty.event_queue").Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if events != 1 || queued != 1 {
		t.Fatalf(
			"the write of a role holding no EXECUTE landed events=%d queue=%d, want 1 each: "+
				"trigger invocation does not go through EXECUTE", events, queued,
		)
	}
}

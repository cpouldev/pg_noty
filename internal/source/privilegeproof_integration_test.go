//go:build integration

package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// theInsufficientPrivilegeCode is PostgreSQL's insufficient_privilege. The low-privilege refusals
// are pinned to it because `err != nil` is equally satisfied by a typo in the table name, a closed
// pool or a syntax error -- none of which establishes the security half these cases exist for.
const theInsufficientPrivilegeCode = "42501"

// assertRefusedForWantingPrivilegeOn runs one statement as the low-privilege role and requires the
// refusal to be a permission error naming the service schema. Naming it is the point: a refusal
// that named only the table would leave open a role holding USAGE on the schema itself.
func assertRefusedForWantingPrivilegeOn(t *testing.T, writer *pgxpool.Pool, what, statement, schemaName string) {
	t.Helper()
	_, err := writer.Exec(t.Context(), statement)
	if err == nil {
		t.Errorf("the limited role's direct %s of the service schema succeeded", what)
		return
	}
	if got := refusalCodeOf(err); got != theInsufficientPrivilegeCode {
		t.Errorf(
			"the limited role's direct %s was refused with SQLSTATE %q (%v), want the server's "+
				"own permission error %s", what, got, err, theInsufficientPrivilegeCode,
		)
		return
	}
	if !strings.Contains(err.Error(), schemaName) {
		t.Errorf(
			"the limited role's direct %s was refused with %q, which does not name the service "+
				"schema %q", what, err.Error(), schemaName,
		)
	}
}

func TestLowPrivilegeWriterCanFireButCannotTouchServiceSchema(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.proof_target (id int)`)
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Target.Table = "proof_target"
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])

	createTheLimitedRole(t, pool, "proof_writer", "proof-password")
	mustExecOn(t, pool, `GRANT INSERT ON public.proof_target TO proof_writer`)
	writer := openPoolAs(t, asRole(t, harnessConfig(t), "proof_writer", "proof-password"), "proof_writer")

	assertRefusedForWantingPrivilegeOn(
		t, writer, "read",
		`SELECT count(*) FROM noty.events`, harnessSchema,
	)
	assertRefusedForWantingPrivilegeOn(
		t, writer, "write",
		`INSERT INTO noty.events (listener, table_name, operation, payload, txid, occurred_at) `+
			`VALUES ('x', 'y', 'insert', '{}', pg_current_xact_id(), clock_timestamp())`, harnessSchema,
	)

	// The other half, and the one the original brief omitted: the same role, holding nothing on the
	// service schema, still makes the event land -- against the real generated function rather than a
	// fixture standing in for it.
	if _, err := writer.Exec(t.Context(), `INSERT INTO public.proof_target VALUES (1)`); err != nil {
		t.Fatal("limited writer could not fire target trigger: " + err.Error())
	}
	events := countOn(
		t, pool, `SELECT count(*) FROM noty.events WHERE table_name=$1`,
		`"public"."proof_target"`,
	)
	if queued := countOn(t, pool, `SELECT count(*) FROM noty.event_queue`); events != 1 || queued != 1 {
		t.Fatalf("security-definer write landed events=%d queue=%d, want one row in each", events, queued)
	}
}

//go:build integration

package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// theInjectingTableName is the injection class *without* the double quote, which is what makes its
// unquoted rendering legal SQL rather than a syntax error. It exists so one control executes its
// injection instead of merely failing to parse: a control that errors proves the value was hostile,
// and one that drops the planted decoy proves what the quoting authority prevented.
const theInjectingTableName = `orders (id int); DROP TABLE users; -- `

// hostileTriggerStatement renders the CreateTrigger for one target through Generate, so a control
// built from it differs from the real case in the quoting authority and in nothing else.
func hostileTriggerStatement(t *testing.T, table string) string {
	t.Helper()
	request := generationRequest(configOperation("insert"))
	request.Target.Table = table
	sets, err := Generate(request)
	if err != nil || len(sets) != 1 {
		t.Fatalf("generating for %q produced %d object sets: %v", table, len(sets), err)
	}
	return sets[0].CreateTrigger
}

// withoutTheQuotingAuthority is the swap itself: the target as schema.Qualified spells it, replaced
// by the same schema and table written raw. Everything else in the statement is the generator's
// own, so a divergence in the control cannot come from anywhere but the quoting.
func withoutTheQuotingAuthority(t *testing.T, statement, schemaName, table string) string {
	t.Helper()
	qualified, fault := schema.Qualified(schemaName, table)
	if fault != schema.IdentifierOK {
		t.Fatalf("qualify %s.%s: %s", schemaName, table, fault)
	}
	stripped := strings.Replace(statement, qualified, schemaName+"."+table, 1)
	if stripped == statement {
		t.Fatalf(
			"the control is byte-identical to the case: %q does not occur in\n%s",
			qualified, statement,
		)
	}
	return stripped
}

// TestWithoutTheQuotingAuthorityTheHostileTargetIsRefused is the injection claim's control on the
// generated statement itself: the same CreateTrigger, with only the target's quoting removed.
//
// The refusal is a *syntax* error rather than an executed injection, and that is a fact about this
// generator's statement shapes rather than a weakness of the control. Every statement it emits
// continues past the target -- `FOR EACH ROW EXECUTE FUNCTION ...`, `IS '<marker>'` -- so an
// unquoted name cannot end one statement and begin another without leaving the tail unparseable,
// and PostgreSQL parses a simple query whole before running any of it. The control that *executes*
// its injection is the one below, on the statement shape where the target does end the statement.
func TestWithoutTheQuotingAuthorityTheHostileTargetIsRefused(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	plantTheDecoy(t, pool)
	mustExecOn(t, pool, "CREATE TABLE public."+mustQuoteIdentifier(t, theHostileTableName)+" (id int)")

	control := withoutTheQuotingAuthority(
		t,
		hostileTriggerStatement(t, theHostileTableName), "public", theHostileTableName,
	)

	ran, err := executedStatementCount(t.Context(), pool, control)
	if err == nil {
		t.Fatalf(
			"the unquoted control ran %d statement(s) and was accepted, so the quoting "+
				"authority prevents nothing this case can observe:\n%s", ran, control,
		)
	}
	if got := refusalCodeOf(err); got != theSyntaxErrorCode {
		t.Fatalf(
			"the unquoted control was refused with SQLSTATE %q (%v), want the server's own "+
				"syntax error %s; any other refusal means the control failed for an unrelated reason",
			got, err, theSyntaxErrorCode,
		)
	}
	assertTheDecoySurvived(t, pool)
}

// TestWithoutTheQuotingAuthorityAnInjectingTableNameExecutesItsInjection is the control that
// executes. Both halves run the same interpolation into the same statement shape and differ only in
// whether the name passes through schema.Quoted, so the second half is what says the quoting is
// what stopped it rather than the name being harmless.
func TestWithoutTheQuotingAuthorityAnInjectingTableNameExecutesItsInjection(t *testing.T) {
	skipIfShort(t)
	t.Run(
		"unquoted", func(t *testing.T) {
			pool := freshDatabase(t)
			plantTheDecoy(t, pool)

			control := "CREATE TABLE public." + theInjectingTableName + " (id int)"
			ran, err := executedStatementCount(t.Context(), pool, control)
			if err != nil {
				t.Fatalf("the unquoted control did not execute, so it demonstrates nothing: %v", err)
			}
			if ran < 2 {
				t.Fatalf(
					"the unquoted control ran %d statement(s); the injection's own second "+
						"statement did not run, so this control cannot fail:\n%s", ran, control,
				)
				return
			}
			if decoySurvives(t, pool) {
				t.Fatal(
					"the unquoted control ran its DROP and the users decoy is still there, so it " +
						"demonstrates nothing about what the quoting authority prevents",
				)
			}
		},
	)

	t.Run(
		"through the quoting authority", func(t *testing.T) {
			pool := freshDatabase(t)
			plantTheDecoy(t, pool)

			execAsExactlyOneStatement(
				t, pool,
				"CREATE TABLE public."+mustQuoteIdentifier(t, theInjectingTableName)+" (id int)",
			)
			assertTheDecoySurvived(t, pool)
			if got := countOn(
				t, pool, "SELECT count(*) FROM pg_class c JOIN pg_namespace n "+
					"ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname=$1",
				theInjectingTableName,
			); got != 1 {
				t.Fatalf(
					"the catalog holds %d tables named %q, want the one the quoted statement "+
						"created under its whole name", got, theInjectingTableName,
				)
			}
		},
	)
}

// decoySurvives answers whether public.users is still there, without failing when it is not -- the
// control above needs it gone.
func decoySurvives(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()
	return countOn(
		t, pool, "SELECT count(*) FROM pg_class c JOIN pg_namespace n "+
			"ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname='users'",
	) == 1
}

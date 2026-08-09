//go:build integration

package schema

import (
	"errors"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is criterion 17: what a role missing a privilege this package requires is told, and what
// it leaves behind. There are two such roles and two such privileges, and both are here.
//
// The first is a role that may not create the schema at all. The second is ADR-3's DBA-pre-creates
// path, which is the one that made criterion 17 wider than the CREATE it was written for: an absent
// marker is claimed rather than refused, the claim is a COMMENT ON SCHEMA, and only an owner may
// comment on a schema -- so a role holding exactly the CREATE and USAGE grants criterion 41's set
// names, on a schema somebody else owns, boots as far as step 4's claim and no further.
//
// Three clauses, and the third is the one that separates wrapping from passing through. `errors.Is`
// alone would pass for a message that also rendered the driver's own sentence as its outermost
// text, and neither `permission denied for database notyharness` nor `must be owner of schema noty`
// names the privilege an operator has to grant in the words this package documents. So the driver's
// sentence is measured -- by issuing the same statement as the same role -- and asserted not to be
// what the boot answered with.

const (
	// theLimitedRole may connect and may read the catalogs, and PostgreSQL grants CREATE on a
	// database to nobody by default -- which is the whole fixture.
	theLimitedRole     = "noty_without_create"
	theLimitedPassword = "no_create"
)

// createTheLimitedRole creates the role if the cluster does not already carry it, and
// aLimitedRoleBoot is a pool onto the harness database as it, with the configuration a boot through
// it reads.
//
// Both are one line of composition over grants_integration_test.go's three role helpers, which is
// what Step 16 reported and could not make from its own file set: this file's copies of them
// differed from those only in fixing the role, and a copy that differs by a parameter is two
// behaviours the moment one of them gains a guard.
func createTheLimitedRole(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	mustCreateRole(t, pool, theLimitedRole, theLimitedPassword)
}

func aLimitedRoleBoot(t *testing.T) (*pgxpool.Pool, config.Config) {
	t.Helper()

	cfg := asRole(t, aBootConfiguration(t), theLimitedRole, theLimitedPassword)
	return openPoolAs(t, cfg, theLimitedRole), cfg
}

// TestARoleThatMayNotCreateASchemaIsToldWhichPrivilegeItLacks is criterion 17 at step 5.
func TestARoleThatMayNotCreateASchemaIsToldWhichPrivilegeItLacks(t *testing.T) {
	skipIfShort(t)

	privileged := freshDatabase(t)
	createTheLimitedRole(t, privileged)
	limited, cfg := aLimitedRoleBoot(t)

	before := catalogInventory(t, privileged, noSchemaExcluded)
	refused := Bootstrap(t.Context(), limited, cfg)

	assertPrivilegeRefusal(
		t, refused, cfg.Database.Schema, createSchemaPrivilege,
		theServersOwnRefusalOf(t, limited, createSchemaStatement+mustQuote(t, cfg.Database.Schema)),
	)
	assertInventoryUnchanged(
		t, before, catalogInventory(t, privileged, noSchemaExcluded),
		"a boot by a role that may not create a schema",
	)
}

// TestARoleThatDoesNotOwnAPreCreatedSchemaIsToldItLacksOwnership is criterion 17 at step 4, and it
// is ADR-3's supported minimal-privilege path taken one statement short.
//
// The fixture is the DBA's half of that path, written as a DBA would: the schema exists and is owned
// by somebody else, and the pg_noty role holds the two schema grants criterion 41's set names. What
// it does not hold is that set's first statement, ALTER SCHEMA ... OWNER TO -- so it may create
// every object it needs and may not comment on the schema holding them, which is the one thing step
// 4's claim does. Until that condition was classified the boot answered the driver's `must be owner
// of schema` verbatim, which names neither this package's vocabulary nor the statement an operator
// runs.
func TestARoleThatDoesNotOwnAPreCreatedSchemaIsToldItLacksOwnership(t *testing.T) {
	skipIfShort(t)

	privileged := freshDatabase(t)
	createTheLimitedRole(t, privileged)
	limited, cfg := aLimitedRoleBoot(t)
	preCreateForTheLimitedRole(t, privileged, cfg.Database.Schema)

	before := catalogInventory(t, privileged, noSchemaExcluded)
	refused := Bootstrap(t.Context(), limited, cfg)

	assertPrivilegeRefusal(
		t, refused, cfg.Database.Schema, claimSchemaPrivilege,
		theServersOwnRefusalOfTheClaim(t, limited, cfg),
	)
	assertInventoryUnchanged(
		t, before, catalogInventory(t, privileged, noSchemaExcluded),
		"a boot by a role that may not claim the schema it was given",
	)
}

// preCreateForTheLimitedRole is what a DBA following ADR-3 runs before pg_noty first starts, minus
// the ownership transfer. CREATE on the *database* is granted as well, so that step 5's `CREATE
// SCHEMA IF NOT EXISTS` cannot be what refuses: both refusals arrive under one SQLSTATE, and a case
// that could be answered by either says nothing about the one it is named for.
func preCreateForTheLimitedRole(t *testing.T, privileged *pgxpool.Pool, schemaName string) {
	t.Helper()

	quotedSchema := mustQuote(t, schemaName)
	mustExecOn(t, privileged, "CREATE SCHEMA "+quotedSchema)
	mustExecOn(t, privileged, "GRANT CREATE, USAGE ON SCHEMA "+quotedSchema+" TO "+theLimitedRole)
	mustExecOn(t, privileged, "GRANT CREATE ON DATABASE "+harnessDatabase+" TO "+theLimitedRole)
}

// theServersOwnRefusalOf is what the driver says when the same role issues the same statement. It
// is measured rather than transcribed, so a release that rewords the message cannot leave the
// comparison below asserting a sentence the server no longer produces.
func theServersOwnRefusalOf(t *testing.T, limited *pgxpool.Pool, statement string) string {
	t.Helper()

	_, err := limited.Exec(t.Context(), statement)
	if err == nil {
		t.Fatalf(
			"%s ran %s, so it is not the unprivileged role this case needs",
			theLimitedRole, statement,
		)
	}
	return err.Error()
}

// theServersOwnRefusalOfTheClaim is the same measurement for step 4's claim. It goes through
// claimMarker rather than through a COMMENT ON assembled here, so what is measured is the statement
// the boot itself issues -- and it is where the claim's error arriving *unwrapped* is asserted, since
// a claimMarker that finished its own error would answer text carrying no SQLSTATE and nothing
// downstream could classify it.
func theServersOwnRefusalOfTheClaim(t *testing.T, limited *pgxpool.Pool, cfg config.Config) string {
	t.Helper()

	marked, fault := schemaObject(cfg.Database.Schema, cfg.Instance)
	if fault != IdentifierOK {
		t.Fatalf("name the schema %s: it %s", cfg.Database.Schema, fault)
	}
	err := claimMarker(t.Context(), limited, marked)
	if err == nil {
		t.Fatalf(
			"%s commented on schema %s, so it owns it and this case proves nothing",
			theLimitedRole, cfg.Database.Schema,
		)
	}
	if refusal, fromServer := serverRefusalIn(err); !fromServer || refusal.code != insufficientPrivilege {
		t.Fatalf(
			"the claim answered %q, whose condition reads (%+v, %t); the boot classifies on that "+
				"condition, so an error arriving without one leaves it unclassifiable", err, refusal, fromServer,
		)
	}
	return err.Error()
}

// assertPrivilegeRefusal checks criterion 17's three clauses one at a time, so a refusal missing one
// of them fails the clause named for it rather than passing on a neighbour's. The privilege is a
// parameter because it is the only clause the two cases differ in.
func assertPrivilegeRefusal(t *testing.T, refused error, schemaName, privilege, servers string) {
	t.Helper()

	if !errors.Is(refused, ErrPrivilege) {
		t.Fatalf("the boot answered %v, want %v", refused, ErrPrivilege)
	}
	if !strings.Contains(refused.Error(), schemaName) {
		t.Errorf("the refusal %q does not name the schema %q it was refused on", refused, schemaName)
	}
	if !strings.Contains(refused.Error(), privilege) {
		t.Errorf(
			"the refusal %q does not name the privilege %q an operator has to grant",
			refused, privilege,
		)
	}
	if refused.Error() == servers || strings.HasPrefix(refused.Error(), servers) {
		t.Errorf(
			"the boot answered with the driver's own sentence %q as its outermost text, so "+
				"criterion 17's message was passed through rather than wrapped", servers,
		)
	}
}

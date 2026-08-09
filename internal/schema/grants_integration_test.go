//go:build integration

package schema

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Criterion 42's connections, and the server measurement that decides which role name the documented
// grant set can be applied under. grantsrenaming_test.go is the container-free half of that decision
// -- the binding between what is applied here and the contract Step 5 wrote down -- and
// grantsroute_integration_test.go holds the proof itself, in both of its directions.

// reservedRoleName is the SQLSTATE the server raises for a role name under the pg_ prefix, measured
// rather than assumed.
const reservedRoleName = "42939"

// The three helpers below are this package's whole vocabulary for running as somebody other than the
// harness owner. Step 16 wrote them beside a fixed-role copy in bootstrapprivilege_integration_test.go
// and reported the duplication rather than making the edit, its file set not covering that file; the
// copies are now one, and the privilege suite composes these.

// mustCreateRole creates one login role unless the cluster already carries it. A restore drops and
// recreates the database and leaves cluster-wide roles alone, so a plain CREATE ROLE would fail on
// the second test to want one.
func mustCreateRole(t *testing.T, pool *pgxpool.Pool, role, password string) {
	t.Helper()

	mustExecOn(
		t, pool, "DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles "+
			"WHERE rolname = '"+role+"') THEN CREATE ROLE "+mustQuote(t, role)+
			" LOGIN PASSWORD '"+password+"'; END IF; END $$",
	)
}

// asRole is one configuration with the connection string's credentials swapped for a role's. It
// takes the configuration rather than reading the harness's own, because the privilege suite swaps
// credentials into a boot configuration and the grant suite into the plain harness one.
func asRole(t *testing.T, cfg config.Config, role, password string) config.Config {
	t.Helper()

	parsed, err := url.Parse(cfg.Database.URL)
	if err != nil {
		t.Fatalf("read the connection string to run as %s: %v", role, err)
	}
	parsed.User = url.UserPassword(role, password)
	cfg.Database.URL = parsed.String()
	return cfg
}

// openPoolAs opens a pool onto one configuration and closes it with the test. It goes through
// OpenPool so that every connection taken here exercises the production connection path (ADR-2).
func openPoolAs(t *testing.T, cfg config.Config, role string) *pgxpool.Pool {
	t.Helper()

	pool, err := OpenPool(t.Context(), cfg)
	if err != nil {
		t.Fatalf("open a pool as %s: %v", role, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// poolAs is a pool onto the harness database as one role.
func poolAs(t *testing.T, role, password string) *pgxpool.Pool {
	t.Helper()
	return openPoolAs(t, asRole(t, harnessConfig(t), role, password), role)
}

// TestTheGoldensOwnRoleNameIsOneTheServerRefusesToCreate is the measurement the renaming rests on,
// asserted against a server rather than recorded in a comment. A release that stopped reserving the
// prefix would fail here and tell the next contributor the indirection can be deleted.
func TestTheGoldensOwnRoleNameIsOneTheServerRefusesToCreate(t *testing.T) {
	skipIfShort(t)

	_, refused := freshDatabase(t).Exec(t.Context(), "CREATE ROLE "+mustQuote(t, fixtureRole)+" LOGIN")

	var fromServer *pgconn.PgError
	if !errors.As(refused, &fromServer) {
		t.Fatalf("CREATE ROLE %s answered %v, want the server's refusal", fixtureRole, refused)
	}
	if fromServer.Code != reservedRoleName {
		t.Errorf(
			"CREATE ROLE %s was refused with SQLSTATE %s (%s), want %s: the golden's role is "+
				"the reserved-prefix near-miss, and the renaming exists only because of that",
			fixtureRole, fromServer.Code, fromServer.Message, reservedRoleName,
		)
	}
	if !strings.Contains(fromServer.Message, fixtureRole) {
		t.Errorf("the refusal %q does not name the role it refused", fromServer.Message)
	}
}

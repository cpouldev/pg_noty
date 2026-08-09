//go:build integration

package schema

import (
	"errors"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This file is OpenPool against a server: that the pool it returns works, and that a real
// authentication failure loses its credentials on the way out. The container-free half -- which
// passwords a connection string spells, and the parse refusal that renders one -- is pool_test.go.

// The two wrong passwords the refused configuration spells. libpq takes the last, so the parameter
// is the one the server refuses and the userinfo one is a second credential the parse discards --
// which makes it the falsifiable half: an elision handed only what pgx parsed would leave it
// readable wherever the DSN is rendered.
const (
	refusedUserinfoPassword = "wrong-in-the-userinfo"
	refusedQueryPassword    = "wrong-in-the-parameter"
)

// TestSelectOneSucceedsThroughAPoolOpenPoolReturned is SC-5, and it is the reason ADR-2 puts
// OpenPool in this package: every container-backed test in Steps 8 to 16 reaches its database
// through the same function production does.
func TestSelectOneSucceedsThroughAPoolOpenPoolReturned(t *testing.T) {
	skipIfShort(t)

	var answered int
	if err := freshDatabase(t).QueryRow(t.Context(), "SELECT 1").Scan(&answered); err != nil {
		t.Fatalf("SELECT 1 through the pool OpenPool returned: %v", err)
	}
	if answered != 1 {
		t.Errorf("SELECT 1 answered %d", answered)
	}
}

// TestTheDriversOwnAuthenticationFailureCarriesThePasswordItWasRefusedFor is the pin the test below
// rests on. Measured on pgx v5.10.0, a connect failure renders no password in its *text* at all, so
// a text-only assertion on one would pass against an OpenPool that elided nothing. The credential
// is in the chain: ConnectError holds the whole *pgconn.Config, Password included, in an exported
// field.
//
// It also pins that the refusal is an authentication failure and not, say, a mistyped host -- which
// is what makes this the case ADR-11 exists for rather than a connection error that happens to fail.
func TestTheDriversOwnAuthenticationFailureCarriesThePasswordItWasRefusedFor(t *testing.T) {
	skipIfShort(t)

	_, err := pgx.Connect(t.Context(), refusedConfig(t).Database.URL)
	if err == nil {
		t.Fatal("the server accepted a wrong password")
	}

	var refused *pgconn.PgError
	if !errors.As(err, &refused) || refused.Code != "28P01" {
		t.Fatalf(
			"the server answered %v, want SQLSTATE 28P01; this test asserts what happens to "+
				"an authentication failure and this is not one", err,
		)
	}

	var carried *pgconn.ConnectError
	if !errors.As(err, &carried) {
		t.Fatalf("the driver's error is a %T, not the type the severing below is written against", err)
	}
	if carried.Config.Password != refusedQueryPassword {
		t.Errorf(
			"ConnectError.Config.Password reads %q, not the password the server refused; the "+
				"severing below would pass against a finishing point that severed nothing",
			carried.Config.Password,
		)
	}
}

// TestOpenPoolLosesEveryPasswordFromARealAuthenticationFailure is SC-6: the case ADR-11 exists for,
// asserted against an error pgx itself produced against a real server rather than one this test
// constructed. Both halves are checked -- the rendered text, and the chain the text cannot speak
// for -- because the pin above measured the credential to be in the second.
func TestOpenPoolLosesEveryPasswordFromARealAuthenticationFailure(t *testing.T) {
	skipIfShort(t)

	pool, err := OpenPool(t.Context(), refusedConfig(t))
	if pool != nil {
		t.Error("OpenPool returned a pool alongside an authentication failure")
	}
	if err == nil {
		t.Fatal("OpenPool accepted a wrong password")
	}

	for _, secret := range []string{refusedUserinfoPassword, refusedQueryPassword} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the finished refusal %q still reads %q", err, secret)
		}
	}

	var reached *pgconn.ConnectError
	if errors.As(err, &reached) {
		t.Errorf(
			"the refusal unwraps back to the driver's, whose Config.Password reads %q",
			reached.Config.Password,
		)
	}
}

// TestTheRefusedConnectionStaysDiagnosableAfterItsPasswordsAreGone is the other side of the
// elision. Removing the password is worth nothing if it takes the message with it: an operator has
// to be able to see which role, which host and which database were refused.
//
// The scheme is not among them, and cannot be: pgx renders a connect failure from the parsed
// configuration rather than from the string, so no scheme appears in one. It is asserted on the
// path that does render the string, by
// TestOpenPoolLosesTheQueryPasswordFromTheDriversOwnRefusal.
func TestTheRefusedConnectionStaysDiagnosableAfterItsPasswordsAreGone(t *testing.T) {
	skipIfShort(t)

	refused := refusedConfig(t)
	_, err := OpenPool(t.Context(), refused)
	if err == nil {
		t.Fatal("OpenPool accepted a wrong password")
	}

	// The host and the port are asserted apart rather than joined: measured on pgx v5.10.0 the
	// message names the *resolved* address and the configured host beside it -- `[::1]:32771
	// (localhost)` for a DSN written `localhost:32771` -- so a row expecting the written spelling
	// would fail against a driver that is telling the operator more, not less.
	host, port := hostAndPortOf(t, refused)
	for _, kept := range []string{harnessUser, harnessDatabase, host, port} {
		if !strings.Contains(err.Error(), kept) {
			t.Errorf(
				"the finished refusal %q no longer names %q, so an operator cannot tell which "+
					"connection was refused", err, kept,
			)
		}
	}
}

// refusedConfig is the harness configuration with both of its passwords wrong. The replacement is
// asserted rather than assumed, because a DSN that still carried the working password would be
// accepted by the server and every test above would fail for the wrong reason.
func refusedConfig(t *testing.T) config.Config {
	t.Helper()

	refused := harnessConfig(t)
	if !strings.Contains(refused.Database.URL, harnessPassword) {
		t.Fatalf(
			"the container's connection string does not spell %s, so this configuration is "+
				"not the harness one with a wrong password", harnessPassword,
		)
	}

	refused.Database.URL = strings.Replace(
		refused.Database.URL, harnessPassword,
		refusedUserinfoPassword, 1,
	) + "&password=" + refusedQueryPassword
	return refused
}

// hostAndPortOf is the host and the port a configuration names, read out of its connection string
// between the userinfo and the database path.
func hostAndPortOf(t *testing.T, cfg config.Config) (host, port string) {
	t.Helper()

	_, after, split := strings.Cut(cfg.Database.URL, "@")
	address, _, hasPath := strings.Cut(after, "/")
	host, port, hasPort := strings.Cut(address, ":")
	if !split || !hasPath || !hasPort {
		t.Fatalf(
			"the harness connection string names no host and port between its userinfo and " +
				"its path, so this test would assert nothing",
		)
	}
	return host, port
}

package schema

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This file is OpenPool's container-free half: which passwords pool.go learns from a connection
// string, and that a driver error carrying one loses it on the way out. The half that needs a
// server -- a usable pool, and a real authentication failure -- is pool_integration_test.go.

// The two spellings a URL can write a password in. They differ deliberately, so a row asserting one
// cannot pass on the other's account.
const (
	userinfoSecret = "userinfo-secret"
	querySecret    = "query-secret"
)

// TestPasswordsWrittenCollectsEverySpellingAURLCanCarry is tripwire-shaped: pgx keeps only the
// effective password (M1 -- `?password=` overrides the userinfo one), so a DSN spelling two
// credentials hands the parse one and discards the other. The discarded one is still written in the
// operator's file.
func TestPasswordsWrittenCollectsEverySpellingAURLCanCarry(t *testing.T) {
	for _, tc := range []struct {
		name, dsn string
		want      []string
	}{
		{name: "no password at all", dsn: "postgres://u@h:5432/db?sslmode=disable"},
		{
			name: "the userinfo password alone", want: []string{userinfoSecret},
			dsn: "postgres://u:" + userinfoSecret + "@h:5432/db",
		},
		{
			name: "a query parameter alone", want: []string{querySecret},
			dsn: "postgres://u@h:5432/db?password=" + querySecret,
		},
		{
			// The one pgx discards comes first, because it is the one a caller reading
			// ConnConfig.Password would never see.
			name: "both, which libpq resolves to the query parameter",
			dsn:  "postgres://u:" + userinfoSecret + "@h:5432/db?password=" + querySecret,
			want: []string{userinfoSecret, querySecret},
		},
		{
			name: "a repeated query parameter, of which libpq takes the last",
			dsn:  "postgres://u@h:5432/db?password=first&password=second",
			want: []string{"first", "second"},
		},
		{
			// %40 is `@`. The rendered error carries the escaped spelling and the server receives
			// the decoded one, so eliding either alone leaves the other readable.
			name: "a percent-encoded query parameter, in both spellings",
			dsn:  "postgres://u@h:5432/db?password=a%40b",
			want: []string{"a%40b", "a@b"},
		},
		{
			name: "the postgresql scheme", want: []string{querySecret},
			dsn: "postgresql://u@h:5432/db?password=" + querySecret,
		},

		// Neither of these is a URL, and the driver redacts every password a keyword string
		// spells -- pinned below by TestTheDriverStillRedactsEveryKeywordPasswordItRenders.
		{name: "a keyword connection string", dsn: "host=h user=u password=" + querySecret},
		{name: "an empty connection string", dsn: ""},
		{name: "a scheme that is not a connection URI", dsn: "https://u:" + userinfoSecret + "@h/db"},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				if got := passwordsWritten(tc.dsn); !slices.Equal(got, tc.want) {
					t.Errorf("passwordsWritten(%q) = %v, want %v", tc.dsn, got, tc.want)
				}
			},
		)
	}
}

// TestEverySpellingTheDriverLeavesReadableIsOneThisFileCollects takes its authority from the
// driver's redaction rather than from passwordsWritten's own branches: a generator closed over the
// code under test can only write the spellings that code already handles. Each row is one clause of
// pgconn's redactPW (errors.go:230 on v5.10.0), and the invariant is the implication: whatever the
// driver renders readable, pool.go must have collected. A version that starts redacting the query
// parameter still passes; one that stops redacting a keyword password fails here by name.
func TestEverySpellingTheDriverLeavesReadableIsOneThisFileCollects(t *testing.T) {
	for _, tc := range []struct{ clause, dsn, secret string }{
		{
			clause: "URL userinfo", secret: userinfoSecret,
			dsn: "postgres://u:" + userinfoSecret + "@h:5432/db?sslmode=nonsense",
		},
		{
			clause: "URL query parameter", secret: querySecret,
			dsn: "postgres://u@h:5432/db?password=" + querySecret + "&sslmode=nonsense",
		},
		{
			clause: "keyword, plain", secret: querySecret,
			dsn: "host=h user=u password=" + querySecret + " sslmode=nonsense",
		},
		{
			clause: "keyword, quoted", secret: "a b",
			dsn: "host=h user=u password='a b' sslmode=nonsense",
		},
	} {
		t.Run(
			tc.clause, func(t *testing.T) {
				rendered := driverRefusalOf(t, tc.dsn)

				if !strings.Contains(rendered, tc.secret) {
					return // the driver redacts this clause; nothing is left for pool.go to elide
				}
				if !slices.Contains(passwordsWritten(tc.dsn), tc.secret) {
					t.Errorf(
						"the driver renders %q readable in %q and passwordsWritten collected %v, "+
							"so this clause leaves a credential in an error message",
						tc.secret, rendered, passwordsWritten(tc.dsn),
					)
				}
			},
		)
	}
}

// TestTheDriverStillRedactsEveryKeywordPasswordItRenders is why passwordsWritten reads URLs only.
// The reliance is on measured behaviour rather than on a reading of the regexes, so a version that
// drops either keyword clause fails here and tells the reader to widen the collector.
func TestTheDriverStillRedactsEveryKeywordPasswordItRenders(t *testing.T) {
	const first, second = "keyword-first", "keyword-second"
	rendered := driverRefusalOf(
		t,
		"host=h user=u password="+first+" password="+second+" sslmode=nonsense",
	)

	for _, secret := range []string{first, second} {
		if strings.Contains(rendered, secret) {
			t.Errorf(
				"the driver renders %q in %q; passwordsWritten reads URLs only and must now "+
					"read the keyword grammar as well", secret, rendered,
			)
		}
	}
}

// driverRefusalOf is the text pgx renders when it refuses a connection string. An unusable sslmode
// is refused during the parse, so no server is involved.
func driverRefusalOf(t *testing.T, dsn string) string {
	t.Helper()

	_, err := pgx.ParseConfig(dsn)
	if err == nil {
		t.Fatalf("the driver accepted %q, so this row has no rendered refusal to read", dsn)
	}
	return err.Error()
}

// TestOpenPoolLosesTheQueryPasswordFromTheDriversOwnRefusal is the falsifiable text case, and it
// needs no server: the refusal below is the one path where pgx renders the connection string, and
// M3 measured that it leaves a query parameter verbatim. The row above it proves the leak is real
// before this one proves it is closed.
func TestOpenPoolLosesTheQueryPasswordFromTheDriversOwnRefusal(t *testing.T) {
	dsn := "postgres://noty:" + userinfoSecret + "@db.internal:5432/notydb" +
		"?password=" + querySecret + "&sslmode=nonsense"
	if !strings.Contains(driverRefusalOf(t, dsn), querySecret) {
		t.Fatal(
			"the driver no longer renders the query password, so this test would pass against " +
				"an OpenPool that elided nothing",
		)
	}

	pool, err := OpenPool(t.Context(), configFor(dsn))
	if pool != nil {
		t.Error("OpenPool returned a pool alongside a refusal")
	}
	if err == nil {
		t.Fatal("OpenPool accepted an unusable sslmode")
	}

	for _, secret := range []string{userinfoSecret, querySecret} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("the finished refusal %q still reads %q", err, secret)
		}
	}
	for _, kept := range []string{"postgres://", "db.internal:5432", "notydb", "sslmode"} {
		if !strings.Contains(err.Error(), kept) {
			t.Errorf(
				"the finished refusal %q no longer names %q, so it is no longer diagnosable",
				err, kept,
			)
		}
	}
}

// TestOpenPoolsRefusalCannotBeUnwrappedBackToTheDriversCopyOfTheDSN closes the half a message-level
// elision leaves open: ParseConfigError holds the connection string in an exported field, whatever
// its Error method renders (M4).
func TestOpenPoolsRefusalCannotBeUnwrappedBackToTheDriversCopyOfTheDSN(t *testing.T) {
	dsn := "postgres://noty:" + userinfoSecret + "@db.internal:5432/notydb?sslmode=nonsense"

	_, err := OpenPool(t.Context(), configFor(dsn))
	var reached *pgconn.ParseConfigError
	if errors.As(err, &reached) {
		t.Errorf(
			"the refusal unwraps back to the driver's, whose ConnString reads %q",
			reached.ConnString,
		)
	}
}

// TestOpenPoolRefusesAConfigurationCarryingNoURL asserts the refusal rather than only its
// absence. An empty string is not a neutral input to the driver: pgx fills it from the
// environment, so accepting one would connect to whatever PGHOST names rather than to the
// database the operator configured.
func TestOpenPoolRefusesAConfigurationCarryingNoURL(t *testing.T) {
	pool, err := OpenPool(t.Context(), config.Config{})

	if pool != nil {
		t.Error("OpenPool returned a pool for a configuration naming no database")
	}
	if err == nil || err.Error() != errNoDatabaseURL.Error() {
		t.Fatalf("OpenPool answered %v, want %v", err, errNoDatabaseURL)
	}
}

// configFor is a configuration naming one connection string, which is all OpenPool reads.
func configFor(dsn string) config.Config {
	return config.Config{Database: config.Database{URL: dsn, Schema: harnessSchema}}
}

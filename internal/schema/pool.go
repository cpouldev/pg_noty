package schema

import (
	"context"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is ADR-2's whole subject and ADR-11's first half: the one place in this module a
// connection string is read, and the one place a pool is built. The packages above this one call
// OpenPool rather than constructing pools of their own, and so does this package's own integration
// suite -- which is the reason ADR-2 put it here rather than in internal/cli's composition root. A
// suite that built its pools differently from production would leave the production connection path
// unexercised until the package that ships it.
//
// Every error leaving this file goes through errorelision.go's finishing point carrying every
// password the connection string spells, and
// TestNoDriverReachingSourceReturnsAnErrorWithoutFinishingIt holds that shut.
// TestPoolIsTheOnlyProductionSourceThatReadsAConnectionString holds the single-reader half.

// errNoDatabaseURL refuses a configuration naming no database.
//
// An empty string is not a neutral input to the driver: pgx fills an absent host, user and database
// from the environment, so accepting one would open a pool against whatever PGHOST names and report
// success. internal/config's R3 makes the key required, and this is the same refusal restated where
// a caller that built its Config by hand still meets it
// (TestOpenPoolRefusesAConfigurationCarryingNoURL).
var errNoDatabaseURL = errors.New("no database URL is configured")

// OpenPool is the connection every later package reaches the database through.
//
// It connects before returning. A pool is lazy by default, so a configuration naming an unreachable
// host or a wrong password would otherwise be reported as a success and fail inside whichever
// statement happened to run first, with the connection failure attributed to that statement.
//
// The pool is the caller's to close.
func OpenPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	dsn := cfg.Database.URL
	if dsn == "" {
		return nil, finished(errNoDatabaseURL)
	}

	// Collected before the refusal is handled rather than after, because the parse is the one path
	// that fails with the connection string in its message. conf is nil there, and credentialsOf
	// answers with what the string itself spells.
	conf, err := pgxpool.ParseConfig(dsn)
	secrets := credentialsOf(dsn, conf)
	if err != nil {
		return nil, finished(err, secrets...)
	}

	pool, err := pgxpool.NewWithConfig(ctx, conf)
	if err != nil {
		return nil, finished(err, secrets...)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, finished(err, secrets...)
	}
	return pool, nil
}

// credentialsOf is every password this package knows about one connection: those the string spells,
// and the one the driver resolved for it.
//
// The resolved one is not always among the written ones, and that is the whole reason it is here.
// Measured on pgx v5.10.0, a string spelling no password at all resolves to PGPASSWORD's value, and
// libpq resolves a passfile entry the same way -- a credential that appears nowhere in the string,
// so only the driver can say what it is
// (TestTheDriverResolvesAPasswordTheConnectionStringDoesNotSpell). It is also the only password a
// keyword-form string contributes, since passwordsWritten reads URIs alone.
//
// resolved is nil when the parse refused the string, which is the one path that renders it.
func credentialsOf(dsn string, resolved *pgxpool.Config) []string {
	written := passwordsWritten(dsn)
	if resolved == nil || slices.Contains(written, resolved.ConnConfig.Password) {
		return written
	}
	return append(written, resolved.ConnConfig.Password)
}

// connectionURISchemes are the two schemes libpq reads a connection URI under, and passwordParameter
// is the parameter one writes a password in.
const (
	passwordParameter = "password"
	postgresScheme    = "postgres"
	postgresqlScheme  = "postgresql"
)

// passwordsWritten is every password a connection string spells, which is not the same as the one
// the driver parses out of it.
//
// pgx keeps the *effective* password only: measured on v5.10.0, `postgres://u:a@h/db?password=b`
// parses to `b` and discards `a`, because libpq takes the last of a repeated setting. The discarded
// one is still a credential the operator wrote in a file, so both are collected and
// errorelision.go's finishing point is variadic to take them.
//
// It reads URIs and not the keyword form, and the boundary is the driver's own redaction rather
// than a guess. Measured on pgconn's redactPW: a keyword `password=` is replaced in every
// occurrence, quoted or plain, while a URI's `?password=` parameter is rendered verbatim. So the
// keyword form has nothing left for this function to find, and that reliance is pinned in both
// directions by TestEverySpellingTheDriverLeavesReadableIsOneThisFileCollects and
// TestTheDriverStillRedactsEveryKeywordPasswordItRenders -- a version that stops redacting the
// keyword form fails there and says to widen this function.
func passwordsWritten(dsn string) []string {
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != postgresScheme && parsed.Scheme != postgresqlScheme) {
		return nil
	}

	var written []string
	if password, spelled := parsed.User.Password(); spelled {
		written = append(written, password)
	}
	return append(written, queryPasswords(parsed.RawQuery)...)
}

// queryPasswords is every password a query string carries, in both the spelling it is written in
// and the spelling the server receives.
//
// Both, because they are read by different things: an error message renders the query exactly as it
// was written, so `?password=a%40b` leaves `a%40b` in the text, while the password the server
// authenticates against -- and the one a caller would recognise -- is `a@b`. Eliding either alone
// leaves the other readable.
//
// The query is split rather than decoded through Query(), because Query() answers with the decoded
// values only and drops the written spelling this function needs.
func queryPasswords(rawQuery string) []string {
	var found []string
	for _, parameter := range strings.Split(rawQuery, "&") {
		written, isPassword := strings.CutPrefix(parameter, passwordParameter+"=")
		if !isPassword {
			continue
		}

		found = append(found, written)
		if received, err := url.QueryUnescape(written); err == nil && received != written {
			found = append(found, received)
		}
	}
	return found
}

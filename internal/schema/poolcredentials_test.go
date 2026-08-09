package schema

import (
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This file covers the half of the credential set the connection string cannot account for. A test
// over the rendered errors alone cannot reach it: measured on pgx v5.10.0 no error text carries the
// *resolved* password, so dropping it from the set breaks nothing any message-level assertion can
// see, while leaving a credential unelided the moment a later version renders one.

// environmentPassword is what PGPASSWORD supplies, and it is deliberately spelled nowhere in the
// connection strings below.
const environmentPassword = "from-the-environment"

// TestTheDriverResolvesAPasswordTheConnectionStringDoesNotSpell is the measurement credentialsOf
// exists for. libpq resolves a password from
// PGPASSWORD and from a passfile, so the effective credential need not appear in the string at all
// -- and passwordsWritten, which reads the string, cannot find one that is not in it.
func TestTheDriverResolvesAPasswordTheConnectionStringDoesNotSpell(t *testing.T) {
	const spellingNoPassword = "postgres://u@h:5432/db?sslmode=disable"
	t.Setenv("PGPASSWORD", environmentPassword)

	if written := passwordsWritten(spellingNoPassword); len(written) != 0 {
		t.Fatalf("the string spells %v, so this file is not measuring what it claims to", written)
	}
	if resolved := parsedConfig(t, spellingNoPassword); resolved.ConnConfig.Password != environmentPassword {
		t.Errorf("the driver resolved %q from a string spelling no password, want %q; if it no "+
			"longer reads the environment, credentialsOf's second half needs re-deriving",
			resolved.ConnConfig.Password, environmentPassword)
	}
}

// TestCredentialsOfCollectsTheResolvedPasswordAsWellAsTheWrittenOnes is the assertion that makes
// the resolved half load-bearing: without it, dropping ConnConfig.Password from the set kills no
// test at all.
func TestCredentialsOfCollectsTheResolvedPasswordAsWellAsTheWrittenOnes(t *testing.T) {
	t.Setenv("PGPASSWORD", environmentPassword)

	for _, tc := range []struct {
		name, dsn string
		refused   bool
		want      []string
	}{
		{
			name: "a string spelling no password, resolved from the environment",
			dsn:  "postgres://u@h:5432/db?sslmode=disable",
			want: []string{environmentPassword},
		},
		{
			// The written one wins, so the set is one password rather than the same one twice.
			name: "a string spelling one, which the driver resolves to the same value",
			dsn:  "postgres://u:" + userinfoSecret + "@h:5432/db?sslmode=disable",
			want: []string{userinfoSecret},
		},
		{
			// libpq takes the last, so the userinfo one is written and discarded while the
			// parameter one is written and resolved -- two written, one resolved, two collected.
			name: "a string spelling two, of which the driver resolves the last",
			dsn:  "postgres://u:" + userinfoSecret + "@h:5432/db?password=" + querySecret,
			want: []string{userinfoSecret, querySecret},
		},
		{
			// passwordsWritten reads URIs alone, so here the resolved password is the only one.
			name: "a keyword string, whose password only the driver reports",
			dsn:  "host=h user=u password=" + querySecret,
			want: []string{querySecret},
		},
		{
			// The path that renders the string is the path with no parsed configuration to ask.
			name:    "a string the parse refused, leaving nothing resolved",
			dsn:     "postgres://u:" + userinfoSecret + "@h/db?password=" + querySecret + "&sslmode=nonsense",
			refused: true,
			want:    []string{userinfoSecret, querySecret},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resolved *pgxpool.Config
			if !tc.refused {
				resolved = parsedConfig(t, tc.dsn)
			}

			if got := credentialsOf(tc.dsn, resolved); !slices.Equal(got, tc.want) {
				t.Errorf("credentialsOf(%q) = %v, want %v", tc.dsn, got, tc.want)
			}
		})
	}
}

// parsedConfig is what the driver made of one connection string, which is the second argument
// OpenPool hands credentialsOf.
func parsedConfig(t *testing.T, dsn string) *pgxpool.Config {
	t.Helper()

	resolved, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("the driver refused %q, so this row has no resolved configuration: %v", dsn, err)
	}
	return resolved
}

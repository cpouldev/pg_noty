package source

import (
	"os"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func TestWhenClauseIsEmptyOrByteTransparent(t *testing.T) {
	for _, tc := range []struct{ name, when, want string }{
		{"empty", "", ""},
		{"parenthesis", "a AND (b)", " WHEN (a AND (b))"},
		{"quote", `status = 'ready'`, ` WHEN (status = 'ready')`},
		{"semicolon", "status = 1; DROP TABLE audit", " WHEN (status = 1; DROP TABLE audit)"},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				if got := whenClause(config.Operation{When: tc.when}); got != tc.want {
					t.Fatalf("whenClause(%q) = %q, want %q", tc.when, got, tc.want)
				}
			},
		)
	}
}

func TestWhenClauseHeaderNamesTheWholeTrustBoundary(t *testing.T) {
	data, err := os.ReadFile("whenclause.go")
	if err != nil {
		t.Fatal(err)
	}
	header := string(data)
	for _, phrase := range []string{
		"DDL time", "pg_noty role", "transaction spanning every listener", "row-write time",
		"every writing transaction", "environment-variable", "internal/config's environment-variable",
		"Pattern 3", "sole statement of its own Exec", "not a security control",
	} {
		if !strings.Contains(header, phrase) {
			t.Errorf("whenclause.go header lacks %q", phrase)
		}
	}
}

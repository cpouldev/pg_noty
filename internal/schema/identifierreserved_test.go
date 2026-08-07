package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheReservedSchemaPrefixIsTheLiteralTheServerReserves pins the value the specification names
// rather than deriving it from the predicate that reads it, which every prefix would satisfy.
func TestTheReservedSchemaPrefixIsTheLiteralTheServerReserves(t *testing.T) {
	if want := "pg_"; reservedSchemaPrefix != want {
		t.Errorf("reservedSchemaPrefix = %s, want %s; PostgreSQL reserves that prefix for system "+
			"schemas and R4 in internal/config refuses it under the same literal",
			reservedSchemaPrefix, want)
	}
}

// TestAServiceSchemaClaimingTheReservedPrefixIsRefused crosses the bound in both directions. The
// three-byte row is the whole point: it is the only input that separates `>=` from `>` on the
// prefix length, and `pg_` on its own is a schema PostgreSQL refuses.
//
// The accepted rows are the guard's other side: each differs from a refused one only in the guarded
// property, so a predicate widened to any name opening with `p`, or to a substring match, fails a
// named row instead of silently refusing valid configuration.
func TestAServiceSchemaClaimingTheReservedPrefixIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name, schema string
		want         bool
	}{
		{name: "the prefix on its own, exactly as long as the bound", schema: "pg_", want: true},
		{name: "a system schema", schema: "pg_catalog", want: true},
		{name: "a plausible service schema under the prefix", schema: "pg_noty", want: true},
		// The server folds an unquoted identifier to lower case before comparing, so a mixed-case
		// spelling reaches the same reserved namespace. This is the only row closing that class.
		{name: "the prefix in upper case", schema: "PG_", want: true},
		{name: "a mixed-case spelling", schema: "Pg_NoTy", want: true},

		{name: "one byte short of the bound", schema: "pg", want: false},
		{name: "the prefix without its underscore", schema: "pgnoty", want: false},
		{name: "one byte", schema: "p", want: false},
		{name: "the empty name, which WhyUnusable answers instead", schema: "", want: false},
		{name: "the suggested schema", schema: "noty", want: false},
		{name: "the prefix somewhere other than the start", schema: "notypg_", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := claimsReservedSchemaPrefix(tc.schema); got != tc.want {
				t.Errorf("claimsReservedSchemaPrefix(%q) = %t, want %t", tc.schema, got, tc.want)
			}
		})
	}
}

// theTwin is the unexported predicate in internal/config this package is forced to duplicate, and
// the file that declares it.
const (
	theTwin     = "usesReservedSchemaPrefix"
	theTwinFile = "ident.go"
)

// TestTheDuplicatedRefusalNamesItsTwinAndStillAgreesWithIt is ADR-11's required mitigation, asserted
// rather than trusted. The twin is unexported and in another package, so the duplication is forced;
// what stops the two copies drifting is that this one names the other and that both are pinned to
// the same literal. Reading the twin's source is the only reach a test has to an unexported symbol
// across a package boundary, and it makes a rename of the twin fail here rather than leave a comment
// pointing at nothing.
func TestTheDuplicatedRefusalNamesItsTwinAndStillAgreesWithIt(t *testing.T) {
	here := string(sourceBytes(t, "identifier.go"))
	if !strings.Contains(here, theTwin) {
		t.Errorf("identifier.go does not name %s, so the one duplication ADR-11 forces is hidden "+
			"rather than recorded", theTwin)
	}

	twin := readTwinSource(t)
	if !strings.Contains(twin, "func "+theTwin+"(") {
		t.Errorf("internal/config/%s no longer declares %s; identifier.go's comment now names "+
			"nothing, so either follow the rename or record that the twin is gone",
			theTwinFile, theTwin)
	}
	if want := `reservedSchemaPrefix = "` + reservedSchemaPrefix + `"`; !strings.Contains(twin, want) {
		t.Errorf("internal/config/%s no longer declares %s, so the two copies of the refusal have "+
			"drifted on the literal they are both pinned to", theTwinFile, want)
	}
}

// readTwinSource reads the frozen file that declares the twin. It is read, never written: the
// duplication exists because the twin cannot be called from here, not because it may be edited.
func readTwinSource(t *testing.T) string {
	t.Helper()

	path := filepath.Join("..", "config", theTwinFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

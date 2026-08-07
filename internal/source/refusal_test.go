package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestGenerationRefusalKindsAreClosedAndPinned(t *testing.T) {
	if got := len(refusalKinds); got != 4 {
		t.Fatalf("refusal inventory has %d entries, want 4", got)
	}
}

func TestIdentifierRefusalNamesValueAndReason(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		reason      schema.IdentifierFault
	}{
		{"empty", "", schema.IdentifierEmpty},
		{"NUL", "ok\x00bad", schema.IdentifierHoldsNUL},
		{"over limit", strings.Repeat("a", schema.MaxIdentifierBytes+1), schema.IdentifierTooLong},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				err := identifierRefusal("target column", tc.value)
				if err == nil || !strings.Contains(err.Error(), tc.value) || !strings.Contains(
					err.Error(),
					string(tc.reason),
				) {
					t.Fatalf("identifierRefusal returned %v, want value and reason %q", err, tc.reason)
				}
				if _, named := err.(unusableIdentifierError); !named {
					t.Fatalf("identifierRefusal returned %T, want unusableIdentifierError", err)
				}
			},
		)
	}
}

// TestIdentifierBoundaryRefusesOnlyPastTheLimit is the 63-byte guard's equality triplet. The 63-byte
// row is the only input separating `len > 63` from `len >= 63`, and the 62-byte row is what fails an
// off-by-one in the other direction.
func TestIdentifierBoundaryRefusesOnlyPastTheLimit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		bytes   int
		refused bool
	}{
		{"RefusalAt62 one under the limit", schema.MaxIdentifierBytes - 1, false},
		{"RefusalAt63 exactly the limit", schema.MaxIdentifierBytes, false},
		{"RefusalAt64 one over the limit", schema.MaxIdentifierBytes + 1, true},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				err := identifierRefusal("target column", strings.Repeat("a", tc.bytes))
				if !tc.refused {
					if err != nil {
						t.Fatalf("a %d-byte identifier was refused: %v", tc.bytes, err)
					}
					return
				}
				if err == nil || !strings.Contains(err.Error(), string(schema.IdentifierTooLong)) {
					t.Fatalf(
						"a %d-byte identifier returned %v, want a refusal naming %q",
						tc.bytes, err, schema.IdentifierTooLong,
					)
				}
			},
		)
	}
}

func TestTargetBoundaryRefusalsNameEachEmptyPart(t *testing.T) {
	valid := Target{Schema: "public", Table: "orders", PrimaryKeyColumns: []string{"id"}}
	for _, tc := range []struct {
		name string
		make func() Target
	}{
		{"schema", func() Target { target := valid; target.Schema = ""; return target }},
		{"table", func() Target { target := valid; target.Table = ""; return target }},
		{"column", func() Target { target := valid; target.PrimaryKeyColumns = []string{""}; return target }},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				err := validateTargetIdentifiers(tc.make())
				if err == nil || !strings.Contains(err.Error(), "empty") {
					t.Fatalf("validateTargetIdentifiers returned %v for empty %s", err, tc.name)
				}
			},
		)
	}
}

func TestTargetBoundaryAccepts63AndRefuses64Bytes(t *testing.T) {
	valid := Target{
		Schema: strings.Repeat("a", schema.MaxIdentifierBytes), Table: "orders", PrimaryKeyColumns: []string{"id"},
	}
	if err := validateTargetIdentifiers(valid); err != nil {
		t.Fatalf("63-byte catalog schema refused: %v", err)
	}
	valid.Schema += "a"
	if err := validateTargetIdentifiers(valid); err == nil || !strings.Contains(
		err.Error(),
		string(schema.IdentifierTooLong),
	) {
		t.Fatalf("64-byte catalog schema returned %v, want named over-limit refusal", err)
	}
}

func TestWhyUnusableReasonIsCarriedWithoutASecondCopy(t *testing.T) {
	value := "bad\x00name"
	err := identifierRefusal("target", value)
	if !strings.Contains(err.Error(), string(schema.WhyUnusable(value))) {
		t.Fatalf("identifier refusal %v does not carry schema.WhyUnusable's reason", err)
	}
}

func TestMissingPrimaryKeyIsNamed(t *testing.T) {
	err := missingPrimaryKey("order_paid")
	if err == nil || !strings.Contains(err.Error(), "order_paid") || !strings.Contains(err.Error(), "primary key") {
		t.Fatalf("missingPrimaryKey returned %v, want listener and primary key", err)
	}
}

func TestRequestValidationRefusesBeforeAnyStatement(t *testing.T) {
	request := Request{
		ServiceSchema: "", Listener: structListener("listener"), Target: Target{Schema: "public", Table: "orders"},
	}
	if err := validateRequestIdentifiers(request); err == nil || !strings.Contains(err.Error(), "service schema") {
		t.Fatalf("validateRequestIdentifiers returned %v, want empty service-schema refusal", err)
	}
}

func structListener(name string) config.Listener { return config.Listener{Name: name} }

// theCatalogReachabilityTerms are the facts the branch has to state: the two
// catalog columns an identifier can arrive from, the PostgreSQL type that bounds them, and both
// properties of that type. A statement is only discoverable by reading, so it is pinned by set
// rather than by one substring.
var theCatalogReachabilityTerms = []string{
	"pg_class.relname", "pg_attribute.attname", "`name`", "NUL-terminated", "63 bytes",
}

// TestTheRefusalBranchStatesWhyItsInputsAreBuiltHere holds the reachability note to the branch it
// explains. Without it, "these inputs cannot come from a catalog" is a fact a reader has to
// rediscover, and the tests that construct them look arbitrary rather than necessary.
func TestTheRefusalBranchStatesWhyItsInputsAreBuiltHere(t *testing.T) {
	if len(theCatalogReachabilityTerms) != 5 {
		t.Fatalf(
			"the note is described by %d terms; update this count with the set",
			len(theCatalogReachabilityTerms),
		)
	}
	stated := string(sourceBytes(t, "refusal.go"))
	for _, term := range theCatalogReachabilityTerms {
		if !strings.Contains(stated, term) {
			t.Errorf(
				"refusal.go no longer states %q, so the branch's reachability is unrecorded "+
					"at the branch it is about", term,
			)
		}
	}
}

package source

import (
	"slices"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestSplitQualifiedInvertsSchemaQualified(t *testing.T) {
	for _, tc := range []struct {
		name, schemaName, tableName string
	}{
		{name: "plain pair", schemaName: "public", tableName: "orders"},
		{name: "dot in schema", schemaName: "sales.eu", tableName: "orders"},
		{name: "embedded quote", schemaName: `a"b`, tableName: "orders"},
		{name: "dollar in table", schemaName: "public", tableName: "orders$1"},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				stored, fault := schema.Qualified(tc.schemaName, tc.tableName)
				if fault != schema.IdentifierOK {
					t.Fatalf("schema.Qualified refused test pair: %q", fault)
				}
				gotSchema, gotTable, ok := SplitQualified(stored)
				if !ok || !slices.Equal([]string{gotSchema, gotTable}, []string{tc.schemaName, tc.tableName}) {
					t.Errorf(
						"SplitQualified(%q) = (%q, %q, %t), want the input pair", stored,
						gotSchema, gotTable, ok,
					)
				}
			},
		)
	}
}

func TestSplitQualifiedRefusesNonQualifiedFormsWithoutPartialValues(t *testing.T) {
	for _, tc := range []struct{ name, stored string }{
		{name: "unquoted", stored: "public.orders"},
		{name: "missing table", stored: `"public".`},
		{name: "unterminated quote", stored: `"public"."orders`},
		{name: "trailing byte", stored: `"public"."orders"x`},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				gotSchema, gotTable, ok := SplitQualified(tc.stored)
				if ok || gotSchema != "" || gotTable != "" {
					t.Errorf(
						"SplitQualified(%q) = (%q, %q, %t), want no partial pair",
						tc.stored, gotSchema, gotTable, ok,
					)
				}
			},
		)
	}
}

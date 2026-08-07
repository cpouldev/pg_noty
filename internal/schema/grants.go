package schema

import (
	"fmt"
	"slices"
	"strings"
)

// This file emits the documented minimal privilege set a DBA applies by hand before pg_noty first
// runs. Three statement families, and nothing else:
//
//   - Ownership of the service schema by the pg_noty role. It stands in for "ownership of the
//     trigger functions" because internal/source creates those functions in this schema as this
//     role, so the outcome is identical and it is expressible without naming an
//     internal/source object.
//   - CREATE and USAGE on that schema, stated explicitly rather than left implicit in ownership, so
//     the set still says what it needs if a DBA transfers ownership elsewhere.
//   - TRIGGER on each distinct target table. Measured necessary: a role that does not own a target
//     table fails CREATE TRIGGER with `permission denied for table ...`.
//
// There is deliberately no statement for the application role. That is the package's
// counter-intuitive deliverable rather than an omission: the trigger functions are SECURITY
// DEFINER, so the application role needs no privilege on the service schema at all, and Step 16
// proves that against a real server using this very text.
//
// Trigger and function names are internal/source's and do not appear. Every identifier is rendered
// through identifier.go.

// targetSeparator divides the two parts of a schema-qualified target table, as configuration writes
// it. internal/config validates the same form with the same separator and refuses a second one;
// that predicate is unexported, so the reading is repeated here rather than shared.
const targetSeparator = "."

// GrantStatements is the exact privilege set the pg_noty role needs, one statement per line, in an
// order that depends only on the values given.
//
// It emits nothing at all when any part of the configuration is unusable. Its signature has no room
// for a reason, and a shorter set is worse than none: a list missing one table's TRIGGER reads as
// complete and is applied, and the omission surfaces in internal/source as a permission denied on
// that table alone.
func GrantStatements(schema, role string, targets []string) []string {
	quotedSchema, quotedRole, usable := grantPrincipals(schema, role)
	if !usable {
		return nil
	}
	tables, readable := grantTargets(targets)
	if !readable {
		return nil
	}

	statements := []string{
		fmt.Sprintf("ALTER SCHEMA %s OWNER TO %s;", quotedSchema, quotedRole),
		fmt.Sprintf("GRANT CREATE ON SCHEMA %s TO %s;", quotedSchema, quotedRole),
		fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s;", quotedSchema, quotedRole),
	}
	for _, table := range tables {
		statements = append(statements,
			fmt.Sprintf("GRANT TRIGGER ON TABLE %s TO %s;", table, quotedRole))
	}
	return statements
}

// grantPrincipals renders the two names every statement carries.
//
// The reserved prefix is asked of the schema alone. R4 refuses it for the service schema, which
// pg_noty creates objects in; a role under that prefix is an ordinary role name -- pg_noty is one --
// and a target table's schema belongs to the customer.
func grantPrincipals(schema, role string) (quotedSchema, quotedRole string, usable bool) {
	if claimsReservedSchemaPrefix(schema) {
		return "", "", false
	}
	quotedSchema, fault := Quoted(schema)
	if fault != IdentifierOK {
		return "", "", false
	}
	quotedRole, fault = Quoted(role)
	if fault != IdentifierOK {
		return "", "", false
	}
	return quotedSchema, quotedRole, true
}

// grantTargets is every distinct target table, rendered and ordered.
//
// The order is the sorted order of the configured names, so it depends on the set and not on the
// order the configuration happened to list them in, nor on a map iteration: the golden this emitter
// is compared against has to hold on every machine.
func grantTargets(targets []string) ([]string, bool) {
	distinct := slices.Clone(targets)
	slices.Sort(distinct)
	distinct = slices.Compact(distinct)

	rendered := make([]string, 0, len(distinct))
	for _, target := range distinct {
		schema, table, found := strings.Cut(target, targetSeparator)
		if !found || strings.Contains(table, targetSeparator) {
			return nil, false
		}
		text, fault := Qualified(schema, table)
		if fault != IdentifierOK {
			return nil, false
		}
		rendered = append(rendered, text)
	}
	return rendered, true
}

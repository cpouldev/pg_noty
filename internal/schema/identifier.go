package schema

import (
	"strings"

	"github.com/jackc/pgx/v5"
)

// This file is the package's sole quoting authority: every configured name reaches DDL through it,
// and no other file may name the driver's Identifier or write a double quote of its own. Three
// scans hold that shut -- TestTheQuotingAuthorityIsTheOnlyFileNamingTheDriversIdentifier,
// TestNoProductionSourceWritesADoubleQuoteOfItsOwn and
// TestNoProductionSourceRendersAValueInGoSyntaxOutsideAnErrorMessage -- because a single choke
// point is what makes the property structural rather than a habit each new file has to remember.
//
// The quoting itself is the driver's, per skill Pattern 7: pgx.Identifier{...}.Sanitize() wraps each
// part in double quotes and doubles every quote inside it, which is PostgreSQL's rule. Bind
// parameters cannot help here -- they protect values, and an object name is not one -- and
// fmt.Sprintf("%q") quotes Go-string-style, where a backslash escapes and an embedded quote would
// close the identifier early.

// The byte limit this file refuses past is objectname.go's MaxIdentifierBytes, read rather than
// re-declared: the two files refuse the same server behaviour from opposite ends -- ObjectName
// truncates a name it generates, this file refuses a name it was given -- and a second copy would drift
// the moment one of them learned something.

// IdentifierFault is why a name cannot be used as a PostgreSQL identifier. Each reason has its own
// answer rather than sharing one negative return, so a caller can say what is actually wrong.
type IdentifierFault string

const (
	IdentifierOK       IdentifierFault = "ok"
	IdentifierEmpty    IdentifierFault = "is empty, and PostgreSQL rejects a zero-length identifier"
	IdentifierHoldsNUL IdentifierFault = "holds a NUL byte, which the driver's quoter deletes"
	IdentifierTooLong  IdentifierFault = "is over the 63-byte identifier limit"
)

// WhyUnusable is the one reason a name cannot become an identifier, or IdentifierOK.
//
// Both refusals answer the same defect: a name the server or the driver would silently rewrite
// names a different object than the operator configured, and nothing downstream can notice.
// Sanitize deletes NUL rather than refusing it, so "no\x00ty" and "noty" render identically
// (pinned by TestSanitizeStillStripsNULRatherThanRefusingIt); the server truncates a name past the
// byte limit with only a NOTICE, so two names sharing a 63-byte prefix become one object.
//
// NUL is tested before the length bound, and deliberately: a name that would be rewritten is
// mis-addressed whatever its length, while a name that is merely long still says what it names. The
// row that violates both asserts that order.
func WhyUnusable(name string) IdentifierFault {
	if name == "" {
		return IdentifierEmpty
	}
	if strings.ContainsRune(name, 0) {
		return IdentifierHoldsNUL
	}
	if len(name) > MaxIdentifierBytes {
		return IdentifierTooLong
	}
	return IdentifierOK
}

// Quoted renders one name as a PostgreSQL identifier, or says why it cannot be one. A
// metacharacter is not a reason: a quote, a semicolon and a comment sequence are all legal inside a
// delimited identifier, and carrying them there inertly is the whole point of quoting.
func Quoted(name string) (string, IdentifierFault) {
	if fault := WhyUnusable(name); fault != IdentifierOK {
		return "", fault
	}
	return sanitized(name), IdentifierOK
}

// Qualified renders a schema-qualified object name as "schema"."name".
//
// The schema is answered first when both parts are unusable, because it is the part an operator
// configures directly while an object name is usually derived from it
// (TestQualifiedAnswersTheSchemaPartBeforeTheNamePart).
func Qualified(schema, name string) (string, IdentifierFault) {
	if fault := WhyUnusable(schema); fault != IdentifierOK {
		return "", fault
	}
	if fault := WhyUnusable(name); fault != IdentifierOK {
		return "", fault
	}
	return sanitized(schema, name), IdentifierOK
}

// sanitized is the module's only call to the driver's quoter. It is reached only through Quoted and
// Qualified, which have already refused the two inputs it answers wrongly, and it is a named
// function of its own so that behaviour can be pinned without a second file naming pgx.Identifier.
func sanitized(parts ...string) string {
	return pgx.Identifier(parts).Sanitize()
}

// reservedSchemaPrefix is the prefix PostgreSQL reserves for system schemas.
const reservedSchemaPrefix = "pg_"

// claimsReservedSchemaPrefix reports whether a name claims the service schema out of the namespace
// PostgreSQL reserves for itself.
//
// This is ADR-11's one forced duplication. Its twin is usesReservedSchemaPrefix in
// internal/config/ident.go, which is unexported and therefore cannot be called from here; both
// copies are pinned to the literal "pg_" and both refuse the exactly-three-byte case, and
// TestTheDuplicatedRefusalNamesItsTwinAndStillAgreesWithIt reads the twin's own source so a rename
// or a change of literal there fails here. Merging the two would mean exporting the config
// predicate, which would put a validation rule into that package's public surface for one caller.
//
// The comparison ignores case because the server folds an unquoted identifier to lower case before
// comparing, so `PG_noty` reaches the same reserved namespace. It is deliberately not folded into
// WhyUnusable: R4 refuses the prefix for the *service schema*, and a target table or a role under
// it is the customer's business.
//
// The length test reads `>=` so that `pg_` on its own -- a schema PostgreSQL refuses -- is refused
// here too, and it is what keeps the slice in bounds for a shorter name.
func claimsReservedSchemaPrefix(name string) bool {
	return len(name) >= len(reservedSchemaPrefix) &&
		strings.EqualFold(name[:len(reservedSchemaPrefix)], reservedSchemaPrefix)
}

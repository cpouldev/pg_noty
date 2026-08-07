package config

import (
	"net/textproto"
	"net/url"
	"strings"
	"testing"
)

// TestNameRuleNamesWhatIsWrong covers the one charset-and-length rule that `instance` (R2)
// and a listener `name` (R23) share. They are the same rule enforced by the same predicate,
// so the rows below assert it from both fields' side rather than there being two
// implementations to keep in step.
//
// Which half failed is asserted rather than a bare rejection, so R2 and R23 can name it as
// R4 and R26 already can through identDefect: "over the character limit" and "illegal
// character" send an author to different places in their file.
//
// The pattern's upper bound is 41 characters, not 40: `^[a-z][a-z0-9_]{0,40}$` is one
// leading letter plus at most 40 more. Both readings are pinned below -- 40 accepted, 41
// accepted, 42 rejected -- so no row depends on which of them a reader assumed
// (.claude/rules/derive-expected-values.md).
func TestNameRuleNamesWhatIsWrong(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  nameDefect
	}{
		{
			name:  "single character",
			value: "a",
			want:  nameOK,
		},
		{
			name:  "empty",
			value: "",
			want:  nameEmpty,
		},
		{
			// 1 leading letter + 39 = 40 characters.
			name:  "forty characters",
			value: "a" + strings.Repeat("b", 39),
			want:  nameOK,
		},
		{
			// 1 leading letter + 40 = 41 characters, which is what {0,40} permits.
			name:  "forty-one characters, the pattern's upper bound",
			value: "a" + strings.Repeat("b", 40),
			want:  nameOK,
		},
		{
			// 1 leading letter + 41 = 42 characters, one past the upper bound.
			name:  "forty-two characters, one past the upper bound",
			value: "a" + strings.Repeat("b", 41),
			want:  nameTooLong,
		},
		{
			name:  "leading digit",
			value: "1order_paid",
			want:  nameBadCharset,
		},
		{
			name:  "uppercase letter",
			value: "Order_paid",
			want:  nameBadCharset,
		},
		{
			name:  "leading underscore",
			value: "_order_paid",
			want:  nameBadCharset,
		},
		{
			name:  "hyphen",
			value: "order-paid",
			want:  nameBadCharset,
		},
		{
			name:  "trailing digits with an underscore",
			value: "order_paid_2",
			want:  nameOK,
		},
		{
			// The name rule is deliberately ASCII-only, unlike identifierDefect, which
			// admits any letter because it validates the user's existing objects.
			name:  "multi-byte letter",
			value: "ordér",
			want:  nameBadCharset,
		},
		{
			// 42 uppercase letters: past the upper bound as well as no name at all. Every
			// other row violates one half only, so this is the row that pins the
			// charset-before-length order nameRuleDefect documents -- reverse the two and
			// only this row fails, having sent the author counting characters instead of
			// reading them (.claude/rules/pin-documented-precedence.md).
			name:  "over the character limit as well as no name",
			value: strings.Repeat("A", 42),
			want:  nameBadCharset,
		},
		{
			name:  "instance charset rule, the same rule as a listener name",
			value: "prod-eu",
			want:  nameBadCharset,
		},
		{
			// 1 + 41 = 42 characters, asserting the length half from the instance side.
			name:  "instance length rule, the same rule as a listener name",
			value: "p" + strings.Repeat("r", 41),
			want:  nameTooLong,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := nameRuleDefect(tc.value); got != tc.want {
				t.Errorf("nameRuleDefect(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestTheCharsetPatternIsTheNameRuleWithoutItsBound pins the derivation that lets
// nameRuleDefect name a failing half without writing the length bound a second time:
// nameCharsetPattern is namePattern with `{0,40}` lifted to `*` and is otherwise the same
// two character classes (.claude/rules/pin-a-claimed-equivalence.md).
//
// Should either pattern gain a character the other lacks, a rejected value would be named
// after the wrong half -- an uppercase name reported as too long, or a long one reported as
// holding an illegal character.
func TestTheCharsetPatternIsTheNameRuleWithoutItsBound(t *testing.T) {
	withinTheBound := []string{
		"a", "order_paid_2", "", "1order", "Order", "_order", "order-paid", "ordér",
		"a" + strings.Repeat("b", 40), // 41 characters, the bound itself
	}
	for _, value := range withinTheBound {
		if nameCharsetPattern.MatchString(value) != namePattern.MatchString(value) {
			t.Errorf("the two patterns disagree about %q, which is inside the bound, so they differ in charset too",
				value)
		}
	}

	// Length is the one thing they must answer differently about, and past the bound is
	// where that shows.
	pastTheBound := "a" + strings.Repeat("b", 41) // 42 characters
	if !nameCharsetPattern.MatchString(pastTheBound) {
		t.Errorf("nameCharsetPattern rejected %q, so it carries a bound of its own", pastTheBound)
	}
	if namePattern.MatchString(pastTheBound) {
		t.Errorf("namePattern accepted %q, which is one character past its bound", pastTheBound)
	}
}

// TestIdentifierDefectNamesWhatIsWrong covers the identifier rule R4 (`database.schema`)
// and R26 (both parts of a `table`) share. The length bound is measured in bytes, which
// the multi-byte rows are what prove: PostgreSQL truncates at 63 bytes with only a
// NOTICE, so a rune-based check lets two long names collide onto one object silently.
func TestIdentifierDefectNamesWhatIsWrong(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  identDefect
	}{
		{
			name:  "lowercase word",
			value: "orders",
			want:  identOK,
		},
		{
			name:  "leading underscore",
			value: "_private",
			want:  identOK,
		},
		{
			// Unquoted identifiers fold to lower case rather than being refused, so
			// refusing this here would be a refusal the server does not make.
			name:  "uppercase letter",
			value: "Orders",
			want:  identOK,
		},
		{
			name:  "dollar sign after the first character",
			value: "order$1",
			want:  identOK,
		},
		{
			name:  "empty",
			value: "",
			want:  identEmpty,
		},
		{
			name:  "leading digit",
			value: "1orders",
			want:  identBadCharset,
		},
		{
			name:  "leading dollar sign",
			value: "$orders",
			want:  identBadCharset,
		},
		{
			name:  "hyphen",
			value: "order-lines",
			want:  identBadCharset,
		},
		{
			name:  "space",
			value: "order lines",
			want:  identBadCharset,
		},
		{
			// 1 leading letter + 62 = 63 bytes, the bound itself.
			name:  "sixty-three bytes",
			value: "a" + strings.Repeat("b", 62),
			want:  identOK,
		},
		{
			// 1 leading letter + 63 = 64 bytes, one past the bound.
			name:  "sixty-four bytes",
			value: "a" + strings.Repeat("b", 63),
			want:  identTooLong,
		},
		{
			// 31 runes x 2 bytes = 62 bytes. This is the accepting side of the
			// multi-byte pair, and it is what makes the rejecting side below
			// falsifiable: the charset admits a letter with a diacritic, so only the
			// byte count can decide between these two rows
			// (.claude/rules/test-both-sides-of-an-exclusion-guard.md).
			name:  "thirty-one multi-byte letters, sixty-two bytes",
			value: strings.Repeat("é", 31),
			want:  identOK,
		},
		{
			// 32 runes x 2 bytes = 64 bytes: 32 is far below 63 runes, so a
			// rune-counting length check accepts this and only a byte-counting one
			// rejects it (.claude/rules/partition-named-test-cases.md).
			name:  "thirty-two multi-byte letters, sixty-four bytes",
			value: strings.Repeat("é", 32),
			want:  identTooLong,
		},
		{
			// 70 hyphens: 7 bytes past the limit as well as no identifier at all. Every
			// row above violates one rule only, so this is the row that pins the
			// charset-before-length order identifierDefect documents -- reverse the two
			// checks and only this row fails
			// (.claude/rules/pin-documented-precedence.md).
			name:  "over the byte limit as well as no identifier",
			value: strings.Repeat("-", 70),
			want:  identBadCharset,
		},
		{
			// PostgreSQL's lexer takes every byte above ASCII as an identifier
			// character, so the server accepts this name; identifierDefect admits only
			// letters up there and refuses it. That narrowing is deliberate and
			// disclosed at identifierDefect, and this row is what makes widening it to
			// match the server a deliberate act rather than a silent one.
			name:  "non-letter symbol above ASCII",
			value: "order€",
			want:  identBadCharset,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := identifierDefect(tc.value); got != tc.want {
				t.Errorf("identifierDefect(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestReservedSchemaPrefixIsRefused covers R4's prefix half. The rejecting rows prove the
// refusal fires; the accepting rows are the nearest names that must survive it, so a
// widened comparison fails here instead of refusing every legitimate schema
// (.claude/rules/test-both-sides-of-an-exclusion-guard.md).
func TestReservedSchemaPrefixIsRefused(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		want   bool
	}{
		{
			name:   "the name this project was originally going to use",
			schema: "pg_noty",
			want:   true,
		},
		{
			name:   "a system schema",
			schema: "pg_catalog",
			want:   true,
		},
		{
			// An unquoted identifier folds to lower case before the server compares it,
			// so the fold is what decides, not the author's capitalisation.
			name:   "uppercase spelling of the prefix",
			schema: "PG_noty",
			want:   true,
		},
		{
			// Exactly as long as the prefix, which is the one value that tells the `>=` in
			// hasPrefixFold's length guard from a `>`: every other rejecting row here is
			// shorter than the prefix and every accepting one is longer, so both readings
			// answer all of them alike
			// (.claude/rules/pin-the-equality-case-of-a-length-guard.md). The expectation is
			// derived from the rule rather than from a run: a value that *is* the prefix
			// opens with it, and PostgreSQL's refusal examines the prefix rather than what
			// follows it.
			name:   "the prefix on its own, as long as the guard's bound",
			schema: "pg_",
			want:   true,
		},
		{
			name:   "the suggested schema",
			schema: "noty",
			want:   false,
		},
		{
			name:   "the prefix without its underscore",
			schema: "pgnoty",
			want:   false,
		},
		{
			name:   "a word merely containing the prefix",
			schema: "app_pg_data",
			want:   false,
		},
		{
			// A `schema:` key with no value reaches this predicate as an empty string,
			// and a value shorter than the prefix is what the length guard in
			// hasPrefixFold exists for: without it the slice panics rather than
			// answering false. This row and the next cover that purpose; what pins the
			// comparison itself is the equality row above, which every table using the
			// shared guard carries one of.
			name:   "empty, shorter than the prefix",
			schema: "",
			want:   false,
		},
		{
			name:   "two characters, shorter than the prefix",
			schema: "pg",
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := usesReservedSchemaPrefix(tc.schema); got != tc.want {
				t.Errorf("usesReservedSchemaPrefix(%q) = %v, want %v", tc.schema, got, tc.want)
			}
		})
	}
}

// TestTheReservedSchemaRefusalCarriesTheServersReasoning pins the two texts a diagnostic
// about the reserved prefix is built from: the server's own explanation, and the schema
// suggested instead. The suggestion is checked against this file's own predicates rather
// than compared to a literal, so a suggestion that would itself be refused fails here.
func TestTheReservedSchemaRefusalCarriesTheServersReasoning(t *testing.T) {
	for _, phrase := range []string{reservedSchemaPrefix, "system schemas"} {
		if !strings.Contains(reservedSchemaPrefixReason, phrase) {
			t.Errorf("reservedSchemaPrefixReason = %q, which does not explain %q",
				reservedSchemaPrefixReason, phrase)
		}
	}

	if usesReservedSchemaPrefix(suggestedSchema) {
		t.Errorf("suggestedSchema = %q, which is itself refused by the rule it answers", suggestedSchema)
	}
	if got := identifierDefect(suggestedSchema); got != identOK {
		t.Errorf("identifierDefect(suggestedSchema) = %v, want identOK; the suggestion must be usable", got)
	}
}

// TestQualifiedTableFaultNamesThePartAtFault covers R26's parts half. A one-part table
// would generate DDL against whatever search_path happens to be, so the form itself is a
// fault distinct from a fault in either part.
func TestQualifiedTableFaultNamesThePartAtFault(t *testing.T) {
	overLong := "a" + strings.Repeat("b", 63) // 64 bytes

	// Every row states wantDefect, including the rows that expect none: the zero value of
	// identDefect is deliberately not one of its constants, so an omitted expectation
	// would be an unstated one rather than a claim of identOK.
	tests := []struct {
		name       string
		table      string
		wantFault  tableFault
		wantDefect identDefect
	}{
		{
			name:       "schema-qualified table",
			table:      "public.orders",
			wantFault:  tableOK,
			wantDefect: identOK,
		},
		{
			// R4's pg_ refusal applies to database.schema alone: the user's own tables
			// may well live in a pg_-prefixed schema, and refusing them here would be a
			// refusal neither the server nor any rule makes
			// (.claude/rules/test-both-sides-of-an-exclusion-guard.md).
			name:       "table in a system schema",
			table:      "pg_catalog.pg_class",
			wantFault:  tableOK,
			wantDefect: identOK,
		},
		{
			name:       "no schema part",
			table:      "orders",
			wantFault:  tableNotQualified,
			wantDefect: identOK,
		},
		{
			name:       "three parts",
			table:      "db.public.orders",
			wantFault:  tableNotQualified,
			wantDefect: identOK,
		},
		{
			name:       "empty",
			table:      "",
			wantFault:  tableNotQualified,
			wantDefect: identOK,
		},
		{
			name:       "empty schema part",
			table:      ".orders",
			wantFault:  tableSchemaPartInvalid,
			wantDefect: identEmpty,
		},
		{
			name:       "empty table part",
			table:      "public.",
			wantFault:  tableNamePartInvalid,
			wantDefect: identEmpty,
		},
		{
			name:       "schema part is not an identifier",
			table:      "1public.orders",
			wantFault:  tableSchemaPartInvalid,
			wantDefect: identBadCharset,
		},
		{
			name:       "table part is not an identifier",
			table:      "public.order lines",
			wantFault:  tableNamePartInvalid,
			wantDefect: identBadCharset,
		},
		{
			name:       "table part over sixty-three bytes",
			table:      "public." + overLong,
			wantFault:  tableNamePartInvalid,
			wantDefect: identTooLong,
		},
		{
			name:       "schema part over sixty-three bytes",
			table:      overLong + ".orders",
			wantFault:  tableSchemaPartInvalid,
			wantDefect: identTooLong,
		},
		{
			// Every row above has one sound part, so this is the row that pins the
			// schema-before-table order qualifiedTableFault documents: validate the
			// table part first and only this row fails, having sent the author to the
			// wrong half of their value (.claude/rules/pin-documented-precedence.md).
			name:       "both parts invalid",
			table:      "1public.1orders",
			wantFault:  tableSchemaPartInvalid,
			wantDefect: identBadCharset,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fault, defect := qualifiedTableFault(tc.table)

			if fault != tc.wantFault {
				t.Errorf("qualifiedTableFault(%q) fault = %v, want %v", tc.table, fault, tc.wantFault)
			}
			if defect != tc.wantDefect {
				t.Errorf("qualifiedTableFault(%q) defect = %v, want %v", tc.table, defect, tc.wantDefect)
			}
		})
	}
}

// TestQualifiedTableSplitsIntoTwoIndependentlyValidatedParts pins that the two parts are
// validated separately rather than the whole value being matched against one pattern: the
// same defect in either half must be reported against that half.
func TestQualifiedTableSplitsIntoTwoIndependentlyValidatedParts(t *testing.T) {
	schema, table, ok := splitQualifiedTable("public.orders")

	if !ok {
		t.Fatal(`splitQualifiedTable("public.orders") reported no schema-qualified form`)
	}
	if schema != "public" {
		t.Errorf("schema part = %q, want %q", schema, "public")
	}
	if table != "orders" {
		t.Errorf("table part = %q, want %q", table, "orders")
	}

	// The same defect, once in each part, must be attributed to the part that carries it.
	if fault, _ := qualifiedTableFault("1public.orders"); fault != tableSchemaPartInvalid {
		t.Errorf("a bad schema part reported %v, want tableSchemaPartInvalid", fault)
	}
	if fault, _ := qualifiedTableFault("public.1orders"); fault != tableNamePartInvalid {
		t.Errorf("a bad table part reported %v, want tableNamePartInvalid", fault)
	}
}

// TestHTTPFieldNameIsValidatedBeforeCanonicalisation covers R19's name half plus the
// canonical spelling Step 10's merge stores headers under.
//
// The order is load-bearing rather than stylistic: textproto.CanonicalMIMEHeaderKey
// returns an invalid name unchanged (pinned by
// TestCanonicalMIMEHeaderKeyReturnsAnInvalidNameUnchanged), so its result says nothing
// about validity. Every invalid row below therefore asserts an empty canonical form:
// a name that failed validation has no canonical spelling to offer.
func TestHTTPFieldNameIsValidatedBeforeCanonicalisation(t *testing.T) {
	tests := []struct {
		name          string
		field         string
		wantCanonical string
		wantValid     bool
	}{
		{
			name:          "lowercase reserved name",
			field:         "x-pg-noty-event-id",
			wantCanonical: "X-Pg-Noty-Event-Id",
			wantValid:     true,
		},
		{
			name:          "lowercase name",
			field:         "user-agent",
			wantCanonical: "User-Agent",
			wantValid:     true,
		},
		{
			name:          "already canonical",
			field:         "X-Tenant",
			wantCanonical: "X-Tenant",
			wantValid:     true,
		},
		{
			// An underscore is a token character, so this is valid and canonicalises.
			name:          "underscore",
			field:         "x_trace_id",
			wantCanonical: "X_trace_id",
			wantValid:     true,
		},
		{
			// A digit is a token character too, and this is the row that says so: no other
			// field name in this table carries one. The canonical spelling is not the name
			// itself -- canonicalisation upper-cases the character after each hyphen and
			// lower-cases every other, so `MD5` becomes `Md5`.
			name:          "digits",
			field:         "Content-MD5",
			wantCanonical: "Content-Md5",
			wantValid:     true,
		},
		{
			// Every non-alphanumeric character of RFC 9110's `tchar`, written out here
			// rather than read from the pattern that admits them, so that dropping one from
			// the pattern fails this row instead of quietly changing what the row asserts.
			// None of them is a letter, so the canonical spelling is the name itself.
			name:          "every token symbol",
			field:         "!#$%&'*+-.^_`|~",
			wantCanonical: "!#$%&'*+-.^_`|~",
			wantValid:     true,
		},
		{
			name:      "space",
			field:     "x tenant",
			wantValid: false,
		},
		{
			name:      "colon",
			field:     "x-tenant:",
			wantValid: false,
		},
		{
			name:      "tab",
			field:     "x\ttenant",
			wantValid: false,
		},
		{
			// A field name is an ASCII token, so a letter with a diacritic is invalid
			// even though CanonicalMIMEHeaderKey hands it back looking untouched.
			name:      "multi-byte letter",
			field:     "Ünicode",
			wantValid: false,
		},
		{
			name:      "empty",
			field:     "",
			wantValid: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			canonical, valid := canonicalHTTPFieldName(tc.field)

			if valid != tc.wantValid {
				t.Errorf("canonicalHTTPFieldName(%q) valid = %v, want %v", tc.field, valid, tc.wantValid)
			}
			if canonical != tc.wantCanonical {
				t.Errorf("canonicalHTTPFieldName(%q) canonical = %q, want %q",
					tc.field, canonical, tc.wantCanonical)
			}
		})
	}
}

// TestCanonicalMIMEHeaderKeyReturnsAnInvalidNameUnchanged pins the standard-library
// behaviour that forces validation to come first
// (.claude/rules/pin-library-behaviour-you-depend-on.md). Measured against Go 1.24: an
// invalid field name comes back byte-identical, so canonicalisation reports nothing about
// validity. Should a future release start signalling invalidity, the ordering rationale
// recorded at canonicalHTTPFieldName changes, and this fails by name first.
func TestCanonicalMIMEHeaderKeyReturnsAnInvalidNameUnchanged(t *testing.T) {
	for _, invalid := range []string{"x tenant", "x-tenant:", "Ünicode"} {
		if got := textproto.CanonicalMIMEHeaderKey(invalid); got != invalid {
			t.Errorf("textproto.CanonicalMIMEHeaderKey(%q) = %q, want it unchanged", invalid, got)
		}
	}
}

// TestValidFieldNameAdmitsExactlyWhatCanonicalisationDoes pins the relation the two halves of
// canonicalHTTPFieldName rest on: the characters this file accepts in a field name are exactly
// the characters textproto is willing to canonicalise. Were this file the wider of the two, a
// name would pass validation and come back from CanonicalMIMEHeaderKey uncanonicalised -- the
// one outcome the ordering exists to prevent (.claude/rules/pin-a-claimed-equivalence.md).
//
// It also covers every character of RFC 9110's `token` from both sides, which no single field
// name in the table above can: each of the 256 bytes is asserted, so dropping one from
// httpFieldNamePattern or admitting one more fails here.
//
// textproto exports no character test, so its answer is read from behaviour: it returns a name
// holding a character it will not canonicalise unchanged, and `a<char>` canonicalises to
// `A<char>`, so an upper-case first byte is exactly its "this is a field name" answer.
func TestValidFieldNameAdmitsExactlyWhatCanonicalisationDoes(t *testing.T) {
	for candidate := range 256 {
		name := string([]byte{'a', byte(candidate)})

		canonicalises := textproto.CanonicalMIMEHeaderKey(name)[0] == 'A'
		if got := validHTTPFieldName(name); got != canonicalises {
			t.Errorf("validHTTPFieldName(%q) = %v, while textproto canonicalises it = %v",
				name, got, canonicalises)
		}
	}
}

// TestReservedHeaderPrefixIsCaseInsensitive covers R21. Phase 5 generates its own
// X-Pg-Noty- headers, so a user header under that prefix would produce two headers of one
// name on the wire; HTTP field names are case-insensitive, so the author's capitalisation
// cannot be what decides.
func TestReservedHeaderPrefixIsCaseInsensitive(t *testing.T) {
	tests := []struct {
		name  string
		field string
		want  bool
	}{
		{
			name:  "canonical spelling",
			field: "X-Pg-Noty-Event-Id",
			want:  true,
		},
		{
			name:  "lowercase spelling",
			field: "x-pg-noty-event-id",
			want:  true,
		},
		{
			name:  "uppercase spelling",
			field: "X-PG-NOTY-SIGNATURE",
			want:  true,
		},
		{
			// Exactly as long as the prefix, which is what tells the `>=` in the shared
			// length guard from a `>`: `X-Pg-Notyx` below is ten bytes too but answers
			// false under either reading, so this is the only row in this table that
			// separates them (.claude/rules/pin-the-equality-case-of-a-length-guard.md).
			// A header named for the prefix and nothing else claims it as surely as a
			// longer one does.
			name:  "the prefix on its own, as long as the guard's bound",
			field: "X-Pg-Noty-",
			want:  true,
		},
		{
			name:  "an unrelated user header",
			field: "X-Tenant",
			want:  false,
		},
		{
			name:  "the prefix without its trailing hyphen",
			field: "X-Pg-Noty",
			want:  false,
		},
		{
			name:  "one character where the trailing hyphen belongs",
			field: "X-Pg-Notyx",
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := usesReservedHeaderPrefix(tc.field); got != tc.want {
				t.Errorf("usesReservedHeaderPrefix(%q) = %v, want %v", tc.field, got, tc.want)
			}
		})
	}
}

// TestConnectionStringFormIsRecognised covers the format half of R3 (`database.url`) and
// R5 (`database.listen_url`): exactly two forms are accepted, and everything else is
// refused here rather than at connection time in a later phase.
func TestConnectionStringFormIsRecognised(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  connStringForm
	}{
		{
			name:  "postgres URI",
			value: "postgres://user:pass@db:5432/app?sslmode=require",
			want:  connStringURI,
		},
		{
			name:  "postgresql URI",
			value: "postgresql://db/app",
			want:  connStringURI,
		},
		{
			// The designator is a literal prefix, so a bare one is the URI form with
			// every part defaulted -- libpq connects it to the default socket. This is
			// the accepting side of the guard the four rejecting rows below pin
			// (.claude/rules/test-both-sides-of-an-exclusion-guard.md), and it is what
			// rules out a fix that demanded a non-empty host.
			name:  "the designator with nothing after it",
			value: "postgresql://",
			want:  connStringURI,
		},
		{
			// net/url reads this as the scheme `postgres` with an opaque body, so a
			// scheme test calls it a URI. libpq refuses it outright:
			// `invalid connection option "postgres:host"`.
			name:  "postgres scheme with no designator",
			value: "postgres:host=db",
			want:  connStringInvalid,
		},
		{
			// The plausible typo, one slash short. net/url reports the scheme
			// `postgres` and the path `/db/app`; libpq does not recognise it as a
			// connection string at all and passes it on as a bare database name.
			name:  "one slash short of the designator",
			value: "postgres:/db/app",
			want:  connStringInvalid,
		},
		{
			// The slashes are present but not where the designator is, which is what
			// makes the test a prefix rather than a search for `://` anywhere.
			name:  "the designator's slashes past the first path segment",
			value: "postgres:/db://app",
			want:  connStringInvalid,
		},
		{
			// libpq compares the designator byte for byte, so this is not the URI
			// form either -- it also reaches the server as a bare database name. Only
			// net/url folds the scheme, which is why the designator is matched against
			// the value rather than against a parsed scheme.
			name:  "upper-case designator",
			value: "POSTGRES://db/app",
			want:  connStringInvalid,
		},
		{
			name:  "keyword/value string",
			value: "host=db port=5432 dbname=app user=noty",
			want:  connStringKeywordValue,
		},
		{
			name:  "keyword/value string with whitespace around the separator",
			value: "host = db password='a b'",
			want:  connStringKeywordValue,
		},
		{
			// url.Parse refuses this outright ("first path segment in URL cannot
			// contain colon"), so an implementation that treated a parse failure as a
			// refusal would reject a legitimate IPv6 connection string.
			name:  "keyword/value string url.Parse cannot parse",
			value: "hostaddr=::1 dbname=app",
			want:  connStringKeywordValue,
		},
		{
			// libpq takes every character up to the `=` as the keyword and then looks that
			// keyword up, so a pair whose name is a digit is refused when it connects. No
			// parameter name opens with a digit, and `1=2` is arbitrary text far more often
			// than it is a connection string, so the refusal is made here -- which is what
			// this predicate exists for -- rather than deferred to a later phase.
			name:  "a pair whose keyword opens with a digit",
			value: "1=2",
			want:  connStringInvalid,
		},
		{
			// The accepting side of that first-character rule, differing from the row above
			// only in where the digit sits: a digit is legal once the name has begun, and
			// narrowing this further would refuse a pair libpq's own keyword scan reads as
			// one (.claude/rules/test-both-sides-of-an-exclusion-guard.md). What this
			// predicate answers is shape; whether libpq knows the option is settled when it
			// connects.
			name:  "keyword/value string whose keyword carries a digit",
			value: "host2=db",
			want:  connStringKeywordValue,
		},
		{
			// The first character is held to the identifier start class, which admits a
			// letter above ASCII: libpq refuses such an option when it connects, and
			// refusing a shape here that libpq's own keyword scan accepts is the costlier of
			// the two mistakes. This row holds that narrowing to its stated width, so
			// tightening it to ASCII is a deliberate act rather than a silent one.
			name:  "a pair whose keyword opens with a letter above ASCII",
			value: "é=1",
			want:  connStringKeywordValue,
		},
		{
			name:  "another database's URI scheme",
			value: "mysql://db/app",
			want:  connStringInvalid,
		},
		{
			name:  "an http URL",
			value: "https://db.example.com/app",
			want:  connStringInvalid,
		},
		{
			name:  "bare text that is neither form",
			value: "just-a-string",
			want:  connStringInvalid,
		},
		{
			name:  "a value whose keyword is not a keyword",
			value: "not a keyword=value",
			want:  connStringInvalid,
		},
		{
			name:  "empty",
			value: "",
			want:  connStringInvalid,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := connectionStringForm(tc.value); got != tc.want {
				t.Errorf("connectionStringForm(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestDestinationURLDefectNamesWhatFailed covers R36's format half. Both conditions are
// required: a scheme check alone accepts `http:///hook`, which has no host and so cannot
// be delivered to. Each refusal is a distinct value so that Step 9's diagnostic can name
// the specific defect rather than reading "invalid value" (NFR Usability).
//
// The parser's reason is asserted alongside the defect, and asserted empty for every defect
// that is not a parse failure, so a reason cannot be attached to a refusal this file made
// itself. The two texts below are the standard library's, measured against Go 1.24; what
// makes them safe to carry is pinned by TestURLParseWrapsItsReasonWithoutTheValue.
func TestDestinationURLDefectNamesWhatFailed(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		want       urlDefect
		wantReason string
	}{
		{
			name:  "https URL with a host",
			value: "https://example.com/hook",
			want:  urlOK,
		},
		{
			name:  "http URL with a host",
			value: "http://example.com:8080/hook",
			want:  urlOK,
		},
		{
			name:  "wrong scheme",
			value: "ftp://host/x",
			want:  urlBadScheme,
		},
		{
			name:  "no scheme at all",
			value: "example.com/hook",
			want:  urlBadScheme,
		},
		{
			// An unclosed IPv6 host makes url.Parse fail, which returns no URL to read a
			// scheme or host from, so this row also proves the error is checked first.
			name:       "unparseable",
			value:      "http://[::1",
			want:       urlUnparseable,
			wantReason: "missing ']' in host",
		},
		{
			// A second parse failure with a different reason, so a reason hard-coded to
			// satisfy the row above fails here.
			name:       "invalid percent escape",
			value:      "https://example.com/%zz",
			want:       urlUnparseable,
			wantReason: `invalid URL escape "%zz"`,
		},
		{
			name:  "scheme without a host",
			value: "http:///hook",
			want:  urlNoHost,
		},
		{
			// url.Parse accepts the empty string, answering a URL with every part empty, so
			// without a guard of its own an absent value is reported as a scheme problem --
			// the wrong half of R36 to send an author to.
			name:  "empty",
			value: "",
			want:  urlEmpty,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defect, reason := destinationURLDefect(tc.value)

			if defect != tc.want {
				t.Errorf("destinationURLDefect(%q) defect = %v, want %v", tc.value, defect, tc.want)
			}
			if reason != tc.wantReason {
				t.Errorf("destinationURLDefect(%q) reason = %q, want %q", tc.value, reason, tc.wantReason)
			}
		})
	}
}

// TestURLParseWrapsItsReasonWithoutTheValue pins the standard-library structure
// parseFailureReason takes apart (.claude/rules/pin-library-behaviour-you-depend-on.md):
// url.Parse fails with a *url.Error whose own text quotes the whole value, while the reason
// it wraps does not. That is what makes the reason safe to carry into a message and the
// wrapper's text not, so a release that flattened the two would fail here by name.
func TestURLParseWrapsItsReasonWithoutTheValue(t *testing.T) {
	const value = "http://[::1"

	_, err := url.Parse(value)
	if err == nil {
		t.Fatalf("url.Parse(%q) succeeded; the unparseable rows no longer test a parse failure", value)
	}

	failure, wrapped := err.(*url.Error)
	if !wrapped {
		t.Fatalf("url.Parse(%q) returned %T, not the *url.Error parseFailureReason takes apart", value, err)
	}
	if !strings.Contains(err.Error(), value) {
		t.Errorf("*url.Error text %q no longer quotes the value, so nothing needs unwrapping", err)
	}
	if strings.Contains(failure.Err.Error(), value) {
		t.Errorf("the wrapped reason %q quotes the value, which a message must not carry", failure.Err)
	}
}

// forbiddenL1Imports are the three import classes an L1 file may not have. The YAML
// library and its AST would mean a domain rule knew how the document was parsed, and os
// would mean it performed IO.
var forbiddenL1Imports = []struct {
	class   string
	matches func(path string) bool
}{
	{class: "ast", matches: func(path string) bool { return strings.HasSuffix(path, "/ast") }},
	{class: "yaml", matches: func(path string) bool { return strings.Contains(path, "yaml") }},
	{class: "os", matches: func(path string) bool { return path == "os" }},
}

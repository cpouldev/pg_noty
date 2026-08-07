package config

import (
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The predicates in this file answer one question each about a configuration value: is
// this a legal name, does this table name have two parts, would the server refuse this
// schema, is this a valid HTTP field name, is this URL deliverable to. They return facts
// -- a boolean, the split parts, an enumerated reason -- and never diagnostics: which
// RuleID a violation belongs to, how it is worded and which token it anchors on are the
// rule layer's to decide (ADR-6), so no message is authored in two layers.
//
// Nothing here parses YAML, touches an AST or performs IO. URL and header handling come
// from the standard library rather than being re-implemented.

// maxIdentifierBytes is PostgreSQL's identifier limit, NAMEDATALEN - 1.
//
// Verified on PostgreSQL 17.10: a 69-character trigger name is accepted and silently
// truncated to 63 bytes with only a NOTICE, so two long names sharing a prefix collide
// onto one object with nothing in the output saying so. The limit counts *bytes*, which is
// why identifierDefect measures len() rather than runes.
//
// Declared once because every identifier predicate shares it, and because phase 3's object
// name derivation must inherit this bound rather than pick its own.
const maxIdentifierBytes = 63

// namePattern is the charset-and-length rule that `instance` (R2) and a listener `name`
// (R23) share, quoted from the rule table verbatim. It admits one leading letter plus at
// most 40 further characters, so the longest legal name is 41 characters.
//
// It is stricter than an identifier deliberately: these two values are pg_noty's own
// identity strings, embedded in the NOTIFY channel name pg_noty_events_<instance> and in
// the colon-delimited ownership marker, so they have to survive both unquoted.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`)

// nameCharsetPattern is namePattern with its length bound lifted: the same two character
// classes, unbounded. It is what lets a refusal name the half at fault without the bound being
// written down a second time -- a non-empty value this accepts and namePattern does not is too
// long and nothing else -- and TestTheCharsetPatternIsTheNameRuleWithoutItsBound holds the two
// patterns to exactly that relation.
var nameCharsetPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// nameDefect is what is wrong with an `instance` (R2) or a listener `name` (R23), so that
// those two rules can name the half at fault as R4 and R26 already can through identDefect:
// an author reads "over the character limit" and counts, or "illegal character" and looks.
// Every value in circulation comes from nameRuleDefect.
type nameDefect string

const (
	nameOK         nameDefect = "ok"
	nameEmpty      nameDefect = "empty"
	nameBadCharset nameDefect = "illegal character"
	nameTooLong    nameDefect = "over the character limit"
)

// nameRuleDefect reports what is wrong with a value under the rule `instance` and `name`
// share. namePattern decides; the halves below only explain a refusal, so no reading of the
// rule lives outside the pattern R2 and R23 quote.
//
// The charset is named before the length, as identifierDefect names it, so a value that is no
// name at all is not reported as a length problem.
func nameRuleDefect(value string) nameDefect {
	switch {
	case namePattern.MatchString(value):
		return nameOK
	case value == "":
		return nameEmpty
	case !nameCharsetPattern.MatchString(value):
		return nameBadCharset
	default:
		return nameTooLong
	}
}

// identDefect is what is wrong with a candidate identifier. It is a string type for the
// same reason RuleID is: a failure message reads the reason rather than an integer. Every
// value in circulation comes from identifierDefect.
type identDefect string

const (
	identOK         identDefect = "ok"
	identEmpty      identDefect = "empty"
	identBadCharset identDefect = "illegal character"
	identTooLong    identDefect = "over the byte limit"
)

// identifierDefect reports what is wrong with a candidate PostgreSQL identifier, which is
// the rule R4 (`database.schema`) and R26 (both parts of a `table`) share.
//
// The charset is the server's own rule for an *unquoted* identifier (PostgreSQL 17,
// "Identifiers and Key Words"): a letter or underscore first, then letters, digits,
// underscores or dollar signs. Letters outside ASCII count as letters, which is what makes
// the byte limit a real bound rather than a synonym for a character count.
//
// It is deliberately wider than the name rule above. These are the user's existing schemas and
// tables rather than names pg_noty chose, so a value the server accepts must not be
// refused here -- an uppercase spelling among them, which the server folds to lower case
// rather than rejecting.
//
// One narrowing is taken knowingly: PostgreSQL's lexer treats every byte above ASCII as an
// identifier character, while this admits only characters that are letters. All of every
// script's letters are covered; what it refuses is a non-letter symbol above ASCII, which
// is not a table name any configuration is expected to carry. The `non-letter symbol above
// ASCII` row holds that narrowing to its stated width, so widening it to match the server
// is a deliberate change rather than a silent one.
func identifierDefect(value string) identDefect {
	if value == "" {
		return identEmpty
	}

	// The charset is tested first so that a value which is no identifier at all is
	// reported as such rather than as a length problem. Swapping the two is what the
	// `over the byte limit as well as no identifier` row fails on.
	if !isIdentifierText(value) {
		return identBadCharset
	}
	if len(value) > maxIdentifierBytes {
		return identTooLong
	}
	return identOK
}

// isIdentifierText reports whether every character of value is legal at its position in
// an unquoted identifier. An empty value yields false, because DecodeRuneInString answers
// RuneError for it and RuneError is not a letter.
func isIdentifierText(value string) bool {
	first, width := utf8.DecodeRuneInString(value)
	if !isIdentifierStart(first) {
		return false
	}

	for _, char := range value[width:] {
		if !isIdentifierChar(char) {
			return false
		}
	}
	return true
}

// isIdentifierStart reports whether a character may open an unquoted identifier.
func isIdentifierStart(char rune) bool {
	return char == '_' || unicode.IsLetter(char)
}

// isIdentifierChar reports whether a character may continue one. A dollar sign is legal
// only here, never as the first character.
func isIdentifierChar(char rune) bool {
	return isIdentifierStart(char) || isDigit(char) || char == '$'
}

// isDigit is spelled out rather than taken from unicode, whose IsDigit accepts digits from
// every script; PostgreSQL's identifier rule means 0 through 9.
func isDigit(char rune) bool {
	return char >= '0' && char <= '9'
}

// reservedSchemaPrefix is the prefix PostgreSQL keeps for itself.
//
// Verified on PostgreSQL 17.10: `CREATE SCHEMA pg_noty` fails with
// `ERROR: unacceptable schema name "pg_noty"` even as superuser. Refusing it here means
// the author gets a line number instead of a DDL error at apply time.
const reservedSchemaPrefix = "pg_"

// reservedSchemaPrefixReason is the server's own explanation, quoted from the DETAIL line
// of that refusal, so a diagnostic about this prefix states PostgreSQL's reason rather
// than sounding like a pg_noty preference.
const reservedSchemaPrefixReason = `the prefix "pg_" is reserved for system schemas`

// suggestedSchema is the schema pg_noty creates its own objects in when the author has
// not chosen one, and therefore the name to suggest when the reserved prefix is refused.
// Declared beside the refusal it answers so that the suggestion and the built-in default
// cannot come to disagree.
const suggestedSchema = "noty"

// usesReservedSchemaPrefix reports whether a schema name claims the prefix PostgreSQL
// reserves for system schemas (R4).
//
// The comparison ignores case because an unquoted identifier is folded to lower case
// before the server compares it, so `PG_noty` reaches that check as `pg_noty`.
func usesReservedSchemaPrefix(name string) bool {
	return hasPrefixFold(name, reservedSchemaPrefix)
}

// hasPrefixFold reports whether value opens with prefix, ignoring case. It is shared by the
// two reserved-prefix predicates, which differ only in the prefix they carry.
//
// The length test is what keeps the slice in bounds. A value shorter than the prefix is a
// real input -- a `schema:` key with no value arrives here empty -- and would otherwise
// panic rather than answer false.
//
// It admits the value that *is* the prefix, which is why the comparison reads `>=`: `pg_` is a
// schema PostgreSQL refuses and `X-Pg-Noty-` is a header phase 5 owns. Both callers' tables
// carry the row where the two lengths are equal, which is the only input that tells `>=` from
// `>` (.claude/rules/pin-the-equality-case-of-a-length-guard.md).
func hasPrefixFold(value, prefix string) bool {
	return len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix)
}

// tableSeparator divides the two parts of a schema-qualified table name.
const tableSeparator = "."

// tableFault is what is wrong with a `table` value: its form, or one of its two parts.
type tableFault string

const (
	tableOK                tableFault = "ok"
	tableNotQualified      tableFault = "not schema-qualified"
	tableSchemaPartInvalid tableFault = "schema part"
	tableNamePartInvalid   tableFault = "table part"
)

// qualifiedTableFault reports whether a `table` value is usable (R26) and, when it is not,
// which half is at fault and why. The second result is identOK unless a part is at fault.
//
// The form is a fault in its own right because a one-part table would generate DDL against
// whatever `search_path` happened to hold, against a table nobody named.
func qualifiedTableFault(value string) (tableFault, identDefect) {
	schema, table, ok := splitQualifiedTable(value)
	if !ok {
		return tableNotQualified, identOK
	}

	// A value with two faults is reported against its schema part, so that the half named
	// is the first one the author reads. Reversing the two checks is what the `both parts
	// invalid` row fails on.
	if defect := identifierDefect(schema); defect != identOK {
		return tableSchemaPartInvalid, defect
	}
	if defect := identifierDefect(table); defect != identOK {
		return tableNamePartInvalid, defect
	}
	return tableOK, identOK
}

// splitQualifiedTable splits a `schema.table` value into its two parts. ok is false unless
// the value carries exactly one separator, so a bare `orders` and a three-part name are
// both refused on form.
//
// The parts come back unvalidated; qualifiedTableFault validates them one at a time, which
// is what lets a fault be attributed to the half that carries it.
func splitQualifiedTable(value string) (schema, table string, ok bool) {
	schema, table, found := strings.Cut(value, tableSeparator)
	if !found || strings.Contains(table, tableSeparator) {
		return "", "", false
	}
	return schema, table, true
}

// httpFieldNamePattern is RFC 9110's `token`, which is what a field name is: one or more
// `tchar` -- a letter, a digit or one of fifteen symbols. That production is written in the
// specification as a list of characters, so it is written here as one, with the hyphen first
// because that is where a character class takes a literal hyphen.
//
// Hand-rolled because the standard library exports no field-name validator: the one net/http
// uses is golang.org/x/net/http/httpguts.ValidHeaderFieldName, which is outside this project's
// closed dependency list. A field name is ASCII, so every character above it fails this class
// exactly as a space or a colon does, and TestValidFieldNameAdmitsExactlyWhatCanonicalisationDoes
// holds the class, character by character, to the one net/textproto is willing to canonicalise.
var httpFieldNamePattern = regexp.MustCompile("^[-!#$%&'*+.^_`|~0-9A-Za-z]+$")

// canonicalHTTPFieldName returns the canonical spelling of a valid HTTP field name, and
// reports false for a name that is not a field name at all (R19).
//
// Validity is tested *before* canonicalising, and the canonical form is computed only on
// the valid path, so the order cannot be quietly reversed later. That order is
// load-bearing rather than stylistic: textproto.CanonicalMIMEHeaderKey returns an invalid
// name **unchanged** -- measured, and pinned by
// TestCanonicalMIMEHeaderKeyReturnsAnInvalidNameUnchanged -- so its result says nothing
// about validity. Canonicalising first would leave an invalid name indistinguishable from
// a valid one, and would hand the merge stage a "canonical" name that is not canonical.
//
// The canonical form is returned rather than left for the caller to recompute, so that
// every layer stores one spelling: `user-agent` in `defaults` and `User-Agent` in a
// listener have to collapse to a single wire header.
func canonicalHTTPFieldName(name string) (string, bool) {
	if !validHTTPFieldName(name) {
		return "", false
	}
	return textproto.CanonicalMIMEHeaderKey(name), true
}

// validHTTPFieldName reports whether name is an RFC 9110 field-name. An empty name is not one:
// the production requires at least one character.
func validHTTPFieldName(name string) bool {
	return httpFieldNamePattern.MatchString(name)
}

// reservedHeaderPrefix is the prefix pg_noty generates its own headers under --
// X-Pg-Noty-Event-Id, -Signature, -Timestamp and -Attempt, which phase 5 owns.
const reservedHeaderPrefix = "X-Pg-Noty-"

// usesReservedHeaderPrefix reports whether a field name claims that prefix (R21), which
// would put two headers of one name on the wire.
//
// The comparison ignores case because HTTP field names do: `x-pg-noty-event-id` and
// `X-Pg-Noty-Event-Id` are the same header, so the author's capitalisation cannot be what
// decides.
func usesReservedHeaderPrefix(name string) bool {
	return hasPrefixFold(name, reservedHeaderPrefix)
}

// connStringForm names which of the two accepted connection-string forms a value is
// written in, or connStringInvalid when it is neither (R3, R5).
type connStringForm string

const (
	connStringInvalid      connStringForm = "neither accepted form"
	connStringURI          connStringForm = "URI"
	connStringKeywordValue connStringForm = "keyword/value"
)

// connectionURIDesignator and shortConnectionURIDesignator are the two prefixes that make a
// value the URI form, named as libpq names them. libpq reads a value as a URI only when it
// opens with one of them byte for byte (fe-connect.c's uri_prefix_length) and reads
// everything else as keyword/value, so the designator rather than a scheme is what decides.
//
// Verified against libpq 18.4: `postgres:host=db` is refused (`invalid connection option
// "postgres:host"`), and `postgres:/db/app` -- a plausible typo -- `postgres:/db://app` and
// `POSTGRES://db/app` are not recognised as connection strings at all, each reaching the
// server as a bare database name. net/url reports the scheme `postgres` for all four, so
// testing the scheme accepts every one of them and defers a refusal this file exists to make
// to connection time in a later phase.
const (
	connectionURIDesignator      = "postgresql://"
	shortConnectionURIDesignator = "postgres://"
)

// connectionStringForm classifies a connection string by form.
//
// Testing the designator keeps url.Parse out of the decision, which the keyword/value form
// requires in its own right: a parse failure must not be a refusal, because url.Parse rejects
// `hostaddr=::1 dbname=app` outright ("first path segment in URL cannot contain colon") and
// that is a perfectly good keyword/value string.
func connectionStringForm(value string) connStringForm {
	if hasConnectionURIDesignator(value) {
		return connStringURI
	}

	if beginsWithConnectionKeyword(value) {
		return connStringKeywordValue
	}
	return connStringInvalid
}

// hasConnectionURIDesignator reports whether a value opens with either designator, and so is
// written in the URI form.
func hasConnectionURIDesignator(value string) bool {
	return strings.HasPrefix(value, connectionURIDesignator) ||
		strings.HasPrefix(value, shortConnectionURIDesignator)
}

// beginsWithConnectionKeyword reports whether a value opens with a libpq `keyword=value`
// pair, which is what tells the keyword/value form apart from arbitrary text.
//
// Only the first pair is inspected, deliberately: libpq permits whitespace around `=` and
// single-quoted values containing spaces, so requiring every whitespace-separated field to
// look like a pair would refuse a legitimate `host=db password='a b'`. What is refused is
// anything that does not open with a keyword and a `=`.
//
// What may sit between a keyword and its value is `connstring.go`'s declaration, read from
// there rather than restated: this predicate decides what R3 accepts as a connection string
// and that file decides where a password inside one can be found, so the two must accept the
// same grammar or R3 would refuse a string redaction can read (Implementation Note 16).
func beginsWithConnectionKeyword(value string) bool {
	from := separatorWidth(value)

	keyword := connectionKeywordWidth(value[from:])
	if keyword == 0 {
		return false
	}

	_, assigned := libpqValueAfter(value, from+keyword)
	return assigned
}

// connectionKeywordWidth is how many bytes of parameter name a string opens with, which is zero
// when it does not open with one at all.
//
// The first character is held to the stricter start class, so `1=2` is not read as a pair: no
// libpq parameter name opens with a digit, libpq refuses a keyword its option list does not
// name, and refusing that here rather than at connection time is what this predicate exists
// for. A digit after the first character stays legal, because libpq's own keyword scan takes
// every character up to the `=` and narrowing further would refuse a pair it reads as one.
//
// That is also why the class is the identifier one rather than ASCII: a keyword opening with a
// letter above ASCII still reads as a pair, and libpq refuses the option when it connects.
// Deferring a refusal costs a later error message, while making one this file cannot justify
// costs a configuration that cannot be written.
func connectionKeywordWidth(text string) int {
	for at, char := range text {
		if at == 0 && !isIdentifierStart(char) {
			return 0
		}
		if !isConnectionKeywordChar(char) {
			return at
		}
	}
	return len(text)
}

// isConnectionKeywordChar reports whether a character may continue a libpq parameter name. The
// identifier charset is reused rather than a third one written, minus the dollar sign no
// parameter name carries.
func isConnectionKeywordChar(char rune) bool {
	return isIdentifierStart(char) || isDigit(char)
}

// urlDefect names why a destination URL was refused (R36). Each refusal is its own value
// so that the rule layer can name the specific defect rather than reading "invalid value"
// (NFR Usability).
type urlDefect string

const (
	urlOK          urlDefect = "ok"
	urlEmpty       urlDefect = "empty"
	urlUnparseable urlDefect = "does not parse"
	urlBadScheme   urlDefect = "scheme is neither http nor https"
	urlNoHost      urlDefect = "no host"
)

// destinationURLDefect reports whether a destination URL can be delivered to, and when it
// cannot, the parser's own reason -- which is empty for every defect this file decides itself.
//
// Both conditions are required, and each is tested on its own. A scheme test alone accepts
// `http:///hook`, which parses and carries the right scheme but names no host, so there is
// nowhere to send to; a host test alone accepts `ftp://host/x`, which phase 5 cannot send
// at all.
//
// Emptiness is answered before the parser is asked, because url.Parse accepts the empty string
// and answers a URL with every part empty: without this guard an absent value would be reported
// as a scheme defect, sending the author to the wrong half of the rule.
func destinationURLDefect(value string) (urlDefect, string) {
	if value == "" {
		return urlEmpty, ""
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return urlUnparseable, parseFailureReason(err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return urlBadScheme, ""
	}
	if parsed.Host == "" {
		return urlNoHost, ""
	}
	return urlOK, ""
}

// parseFailureReason is the parser's explanation of a failure, without the copy of the value
// that url.Error wraps it in: its own text reads `parse "<the whole value>": <reason>`, and a
// message is written to output verbatim, outside the redaction that protects a quoted source
// line. The reason is a fact about the failure; the value is the caller's already.
//
// url.Parse fails only with a *url.Error, which TestURLParseWrapsItsReasonWithoutTheValue pins,
// so the second answer stands for an error shape that cannot arrive rather than for a reason
// that could not be read.
func parseFailureReason(err error) string {
	if failure, wrapped := err.(*url.Error); wrapped {
		return failure.Err.Error()
	}
	return ""
}

package config

import "strings"

// This file is the package's one declaration of libpq's keyword/value grammar: what separates the
// pairs of a connection string, and what may sit between a keyword and its value.
//
// It has one home and two readers, which is the point. `connstring.go` asks where a password inside
// such a string sits; `ident.go`'s R3 asks whether a value is such a string at all. The two had
// encoded the grammar separately and already disagreed about what whitespace counts, so a value R3
// accepted could hold a password redaction could not find (Implementation Note 16).

// libpqSeparators is the whitespace libpq accepts between the pairs of a keyword/value connection
// string and on either side of each pair's `=`. It is C `isspace` under the C locale, which is
// what fe-connect.c's conninfo_parse tests one byte at a time -- deliberately *not* Unicode
// whitespace, so a non-breaking space is not a separator to libpq and must not be one here.
//
// This is the authoritative set for the package. It is named here and read from here, including by
// `ident.go`, rather than each site reaching for strings.TrimSpace and drifting.
const libpqSeparators = " \t\n\v\f\r"

// separatorWidth is how many bytes of libpq separator a string opens with.
func separatorWidth(text string) int {
	return len(text) - len(strings.TrimLeft(text, libpqSeparators))
}

// libpqValueAfter is where the value of a `keyword=value` pair begins, given the offset just past
// its keyword, and whether an `=` follows that keyword at all: past the separators libpq permits
// before the `=`, past the `=`, and past the separators after it, because `password = 's3cret'` is
// as legal as `password=s3cret`.
//
// This is the one place that grammar is written down. `ident.go` asks whether a value opens with a
// pair (R3) and this file asks where a named pair's value begins; both must accept the same syntax,
// or R3 would refuse a string redaction can read, or accept one it cannot.
func libpqValueAfter(text string, afterKeyword int) (int, bool) {
	equals := afterKeyword + separatorWidth(text[afterKeyword:])
	if !strings.HasPrefix(text[equals:], "=") {
		return 0, false
	}

	value := equals + len("=")
	return value + separatorWidth(text[value:]), true
}

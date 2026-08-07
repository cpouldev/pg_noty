package config

import (
	"cmp"
	"slices"
	"strings"
)

// This file answers one question about Postgres connection-string syntax: where in the text do
// the passwords sit. It knows nothing about redaction -- what to put in those spans is the
// redactor's decision, not this file's -- and nothing about rendering.
//
// The grammar it reads a string against is `libpqgrammar.go`'s declaration, shared with `ident.go`'s
// R3 predicate, so what R3 accepts as a connection string and what redaction can find inside one
// cannot drift apart (Implementation Note 16).

// span is a byte range of a string, from inclusive to exclusive.
type span struct {
	from byteOffset
	to   byteOffset
}

// passwordSpans is every span of a connection string that holds a password, ordered right to left
// so that replacing one leaves the offsets of every span still to come exactly where they were
// derived.
//
// Every locator contributes, rather than the first that matches. One string carries a password in
// more than one place routinely: `postgres://u:pw@h/db?sslpassword=keypass` carries two, and libpq
// takes the *last* of a repeated `password=` -- which is precisely the one a first-match-wins scan
// would leave rendered.
//
// Overlapping spans are merged rather than replaced in turn. A userinfo password that is itself
// spelled `password=x` puts both locators on the same bytes, and replacing one would move the
// other's end.
func passwordSpans(text string) []span {
	found := keywordPasswordSpans(text)
	if userinfo, carries := uriPasswordSpan(text); carries {
		found = append(found, userinfo)
	}

	slices.SortFunc(found, func(a, b span) int { return cmp.Compare(a.from, b.from) })
	merged := mergedSpans(found)
	slices.Reverse(merged)
	return merged
}

// mergedSpans joins the spans of an ascending list that overlap or touch, so that no two of the
// spans a caller replaces can move each other's offsets.
func mergedSpans(ascending []span) []span {
	var merged []span

	for _, next := range ascending {
		last := len(merged) - 1
		if last >= 0 && next.from <= merged[last].to {
			merged[last].to = max(merged[last].to, next.to)
			continue
		}
		merged = append(merged, next)
	}
	return merged
}

// uriPasswordSpan is the password of a scheme://user:password@host URI.
//
// The userinfo is taken to end at the last `@` in the whole text rather than at the first
// delimiter after the scheme, because a password may hold any of the characters that would
// otherwise delimit it: a colon, a slash, another `@`, even a second `://`. Taking the last one
// over-redacts a URI whose *path* holds an `@`, and cannot under-redact one whose password does.
func uriPasswordSpan(text string) (span, bool) {
	const schemeSeparator = "://"

	scheme := strings.Index(text, schemeSeparator)
	if scheme < 0 {
		return span{}, false
	}
	authority := scheme + len(schemeSeparator)

	host := strings.LastIndexByte(text, '@')
	if host < authority {
		return span{}, false
	}

	// No colon in the userinfo is a user with no password, and a port's colon is past the
	// `@` rather than inside it, so neither is mistaken for one.
	colon := strings.IndexByte(text[authority:host], ':')
	if colon < 0 {
		return span{}, false
	}
	return span{from: byteOffset(authority + colon + len(":")), to: byteOffset(host)}, true
}

// passwordKeyword is the libpq parameter whose value is a secret.
//
// No boundary is required before it, deliberately: libpq's own `sslpassword` ends in this one, and
// the passphrase of a client key is a secret too, so matching it is the safe direction -- a
// boundary would leave it rendered.
const passwordKeyword = "password"

// keywordPasswordSpans is where a keyword/value connection string -- or the query of a URI, which
// spells its parameters the same way -- holds a password. The span runs from the value to the end of
// the text, unconditionally, and one span therefore covers every later occurrence.
//
// Ending it at the first separator is what libpq does *when the string is a valid one*, and what
// reaches this file is a YAML scalar that may not be a connection string at all. So a password
// holding a space has everything after the space rendered, and no test available here separates the
// two readings: `password=a b` and `password=a dbname=x` are equally "a password containing a space"
// and "a password followed by a field", and only libpq's option list decides between them -- a list
// `ident.go` deliberately does not encode, and one that would not help, because a password may
// contain ` dbname=` as readily as ` A=`.
//
// Two narrower rules were tried and each left a case the containment property then found: end at the
// first separator, and end at the first separator only when another pair follows it. That is what
// makes the unconditional answer the correct one rather than the lazy one.
//
// The cost is bounded and stated: pairs written *after* the password go with it, so
// `host=h password=p dbname=d` renders as `host=h password=[redacted]`. Keywords before it stay
// legible, and a URI's scheme, user, host, port and database name are uriPasswordSpan's extent and
// are untouched -- which is where AC #24 and CK-7 make their must-survive claim.
func keywordPasswordSpans(text string) []span {
	value, assigned := valueAfterKeyword(text, passwordKeyword)
	if !assigned {
		return nil
	}
	return []span{{from: byteOffset(value), to: byteOffset(len(text))}}
}

// valueAfterKeyword is where the value of the pair named keyword begins, and whether the text uses
// that keyword as one at all. A keyword the text merely contains is skipped and the scan
// continues, so a later `password=` is still found.
func valueAfterKeyword(text, keyword string) (int, bool) {
	for at := 0; ; {
		found := strings.Index(text[at:], keyword)
		if found < 0 {
			return 0, false
		}

		afterKeyword := at + found + len(keyword)
		if value, assigned := libpqValueAfter(text, afterKeyword); assigned {
			return value, true
		}
		at = afterKeyword
	}
}

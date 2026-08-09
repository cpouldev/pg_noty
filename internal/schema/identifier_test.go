package schema

import (
	"strings"
	"testing"
)

// hostileNames are the metacharacters a configured name can carry into DDL. Each is written the way
// a YAML author could write it, and each is asserted inert rather than merely quoted: the oracle
// below reads the rendering back the way PostgreSQL would, so a rendering that closed its quote
// early and left a second statement behind fails here.
var hostileNames = []struct{ name, written, want string }{
	{name: "an embedded double quote", written: `no"ty`, want: `"no""ty"`},
	{name: "a statement separator", written: "noty;drop", want: `"noty;drop"`},
	{name: "a comment sequence", written: "noty--x", want: `"noty--x"`},
	{name: "a dot, which does not split one part", written: "no.ty", want: `"no.ty"`},
	{
		name:    "a quote, a separator and a comment sequence at once",
		written: `noty"; DROP TABLE orders; --`,
		want:    `"noty""; DROP TABLE orders; --"`,
	},
}

// TestEveryHostileNameRendersAsOneInertIdentifier asserts both halves of the Security NFR: the
// exact text, derived from the quoting rule (wrap in quotes, double every interior quote), and that
// the text reads back as exactly one identifier naming exactly the configured object.
func TestEveryHostileNameRendersAsOneInertIdentifier(t *testing.T) {
	for _, tc := range hostileNames {
		t.Run(tc.name, func(t *testing.T) {
			rendered, fault := Quoted(tc.written)
			if fault != IdentifierOK {
				t.Fatalf("quoted(%q) refused the name as %q; a metacharacter is legal inside a "+
					"delimited identifier", tc.written, fault)
			}
			if rendered != tc.want {
				t.Errorf("quoted(%q) = %s, want %s", tc.written, rendered, tc.want)
			}
			assertReadsBackAs(t, rendered, tc.written)
		})
	}
}

// TestAQualifiedNameIsTwoInertIdentifiers covers the same hostility in the schema position, which is
// the position the Security NFR names first: every criterion below is about a schema name and a
// table name, not a table name alone.
func TestAQualifiedNameIsTwoInertIdentifiers(t *testing.T) {
	for _, tc := range hostileNames {
		t.Run(tc.name, func(t *testing.T) {
			rendered, fault := Qualified(tc.written, tc.written)
			if fault != IdentifierOK {
				t.Fatalf("qualified(%q, %q) refused both parts as %q", tc.written, tc.written, fault)
			}
			if want := tc.want + "." + tc.want; rendered != want {
				t.Errorf("qualified(%q, %q) = %s, want %s", tc.written, tc.written, rendered, want)
			}
			assertReadsBackAs(t, rendered, tc.written, tc.written)
		})
	}
}

// assertReadsBackAs is the inertness oracle. Byte equality alone cannot say that a rendering is one
// identifier: `"a";DROP` and `"a;DROP"` differ by one character and only the second is inert. This
// reads the rendering with PostgreSQL's own rule and requires the whole string to be consumed as
// exactly the parts that went in.
func assertReadsBackAs(t *testing.T, rendered string, want ...string) {
	t.Helper()

	parts, whole := unquotedIdentifier(rendered)
	if !whole {
		t.Fatalf("%s does not read back as delimited identifiers, so it carries text outside "+
			"them -- which is a second statement, not a name", rendered)
	}
	if len(parts) != len(want) {
		t.Fatalf("%s reads back as %d identifiers %q, want %d", rendered, len(parts), parts, len(want))
	}
	for i, part := range parts {
		if part != want[i] {
			t.Errorf("part %d of %s reads back as %q, want %q -- it names a different object",
				i, rendered, part, want[i])
		}
	}
}

// unquotedIdentifier reads text back the way PostgreSQL reads a delimited identifier: a dot-separated
// run of quoted parts in which a doubled quote is one quote character. It answers the parts and
// whether the whole string was consumed, so trailing text is a refusal rather than a shorter answer.
func unquotedIdentifier(text string) ([]string, bool) {
	var parts []string
	for {
		part, tail, delimited := readDelimitedPart(text)
		if !delimited {
			return nil, false
		}
		parts = append(parts, part)
		if tail == "" {
			return parts, true
		}
		if tail[0] != '.' {
			return nil, false
		}
		text = tail[1:]
	}
}

// readDelimitedPart reads one quoted part and answers the text after it.
func readDelimitedPart(text string) (part, tail string, delimited bool) {
	if len(text) == 0 || text[0] != '"' {
		return "", "", false
	}

	var built strings.Builder
	for i := 1; i < len(text); i++ {
		if text[i] != '"' {
			built.WriteByte(text[i])
			continue
		}
		if i+1 < len(text) && text[i+1] == '"' {
			built.WriteByte('"')
			i++
			continue
		}
		return built.String(), text[i+1:], true
	}
	return "", "", false
}

// TestTheOracleRefusesARenderingThatEscapedItsQuotes gives the oracle above its own falsifiability.
// Without it, an oracle that answered "inert" for everything would make every row of the two tests
// above pass.
func TestTheOracleRefusesARenderingThatEscapedItsQuotes(t *testing.T) {
	for _, tc := range []struct {
		name, rendered string
		wantParts      []string
	}{
		{name: "one plain part", rendered: `"noty"`, wantParts: []string{"noty"}},
		{name: "two parts", rendered: `"noty"."events"`, wantParts: []string{"noty", "events"}},
		{name: "a doubled quote inside", rendered: `"no""ty"`, wantParts: []string{`no"ty`}},
		{name: "a separator inside", rendered: `"noty;drop"`, wantParts: []string{"noty;drop"}},
		// The failures the oracle exists to catch: each is what a broken quoter emits.
		{name: "an unquoted name", rendered: "noty"},
		{name: "a lone quote closing early, leaving a second statement", rendered: `"noty";DROP TABLE x`},
		{name: "a quote never closed", rendered: `"noty`},
		{name: "a Go-style backslash escape rather than a doubled quote", rendered: `"no\"ty"`},
		{name: "a trailing dot with no part after it", rendered: `"noty".`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts, whole := unquotedIdentifier(tc.rendered)
			if whole != (tc.wantParts != nil) {
				t.Fatalf("unquotedIdentifier(%s) read %q, whole = %t", tc.rendered, parts, whole)
			}
			if whole && strings.Join(parts, "\x00") != strings.Join(tc.wantParts, "\x00") {
				t.Errorf("unquotedIdentifier(%s) = %q, want %q", tc.rendered, parts, tc.wantParts)
			}
		})
	}
}

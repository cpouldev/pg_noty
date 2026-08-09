package config

import (
	"strings"
	"testing"
)

// This file is the urlPassword declaration's shape question: what happens when the value written
// beneath `database.url` is not a connection string at all.
//
// The declaration names a grammar, and a grammar reads one shape. A mapping or a list written there
// is a shape that grammar cannot read, so the answer "this text holds no password" is not a finding
// about the text -- it is the grammar declining to apply. Returning the value untouched on that
// answer is the fail-open default D3 forbids, and it was reachable: a flow mapping holding a
// password rendered verbatim while the very same entries written as a block mapping were blanked
// wholesale.

// TestAUrlPasswordValueTheConnectionStringGrammarCannotReadIsBlankedWholesale writes the shapes the
// grammar has no reading for. Each is asserted on its own, because they reach redactionGeometry by
// different routes: a mapping arrives whole, and a sequence arrives split into its elements.
func TestAUrlPasswordValueTheConnectionStringGrammarCannotReadIsBlankedWholesale(t *testing.T) {
	for _, written := range []struct {
		name   string
		source string
	}{
		{
			name:   "a flow mapping",
			source: "database:\n  url: {host: db.internal, password: " + leakSentinel + "FLOW-MAP}\n",
		},
		{
			name:   "a flow sequence",
			source: "database:\n  url: [" + leakSentinel + "FLOW-SEQ]\n",
		},
		{
			name: "a block sequence",
			source: "database:\n  url:\n    - " + leakSentinel + "BLOCK-SEQ-1\n    - " +
				leakSentinel + "BLOCK-SEQ-2\n",
		},
		{
			name: "a flow mapping beneath the other sensitive connection string",
			source: "database:\n  listen_url: {password: " + leakSentinel +
				"LISTEN}\n",
		},
		{
			name:   "a block mapping beneath a misspelt parent, where no declaration can be looked up",
			source: "databse:\n  url:\n    host: h\n    password: " + leakSentinel + "UNKNOWN\n",
		},
	} {
		t.Run(written.name, func(t *testing.T) {
			source := "version: 1\n" + written.source
			if _, pathAware := branchTaken(plantedSecret{text: source}); !pathAware {
				t.Fatalf("the document does not parse, so this case would measure the fallback:\n%s",
					source)
			}

			if rendered := renderEveryLineOf(source); strings.Contains(rendered, leakSentinel) {
				t.Errorf("a urlPassword value written as %s rendered verbatim\nsource:\n%s"+
					"\nrendered:\n%s", written.name, source, rendered)
			}
		})
	}
}

// TestAUrlPasswordScalarKeepsEverythingTheGrammarCanRead is the other side of the guard. Blanking a
// urlPassword value wholesale whenever no password was found would pass every case above and take
// the host, port and database name with it -- which is the one thing D3 says a URL must not lose,
// because a URL blanked whole cannot tell its own diagnostic what it rejected.
func TestAUrlPasswordScalarKeepsEverythingTheGrammarCanRead(t *testing.T) {
	for _, written := range []struct {
		name, value, survives string
	}{
		{
			name:     "a connection string with a password",
			value:    "postgres://noty:" + leakSentinel + "PW@db.internal:5432/noty",
			survives: "db.internal:5432/noty",
		},
		{
			name:     "a connection string with no password",
			value:    "postgres://noty@db.internal:5432/noty",
			survives: "postgres://noty@db.internal:5432/noty",
		},
		{
			name:     "a bare environment reference",
			value:    "${DATABASE_URL}",
			survives: "${DATABASE_URL}",
		},
		{
			name:     "a quoted scalar the grammar finds no password in",
			value:    `"not-a-connection-string"`,
			survives: "not-a-connection-string",
		},
		{
			name:     "a keyword connection string",
			value:    "host=db.internal password=" + leakSentinel + "KW",
			survives: "host=db.internal",
		},
	} {
		t.Run(written.name, func(t *testing.T) {
			source := "version: 1\ndatabase:\n  url: " + written.value + "\n"

			rendered := renderEveryLineOf(source)
			if strings.Contains(rendered, leakSentinel) {
				t.Fatalf("the password was rendered, so the survival below is measured on the "+
					"wrong text:\n%s", rendered)
			}
			if !strings.Contains(rendered, written.survives) {
				t.Errorf("%q was blanked with the value; a scalar is the shape the connection-string "+
					"grammar reads, so it keeps everything but its passwords\nrendered:\n%s",
					written.survives, rendered)
			}
		})
	}
}

// TestAContainerBeneathAKeyDeclaredPublicForDiagnosticsStillRenders bounds the fail-closed arm by
// the declaration rather than by the shape. destination.url carries loggedHolding, so only the
// resolved-config log hides it and a diagnostic quotes it as written -- a container written there
// must therefore still render, or the new arm has widened past the declaration that admits it.
func TestAContainerBeneathAKeyDeclaredPublicForDiagnosticsStillRenders(t *testing.T) {
	for _, written := range []struct {
		name, value, survives string
	}{
		{
			name:     "a flow mapping",
			value:    "{host: hooks.example, path: /x}",
			survives: "hooks.example",
		},
		{
			name:     "a scalar",
			value:    "https://hooks.example/x",
			survives: "https://hooks.example/x",
		},
	} {
		t.Run(written.name, func(t *testing.T) {
			source := "version: 1\nlisteners:\n- name: order_paid\n  destination:\n    url: " +
				written.value + "\n"

			if rendered := renderEveryLineOf(source); !strings.Contains(rendered, written.survives) {
				t.Errorf("destination.url written as %s was redacted; it is declared public for "+
					"diagnostics\nrendered:\n%s", written.name, rendered)
			}
		})
	}
}

package config

import (
	"slices"
	"strings"
	"testing"
)

// Stage F's walk: which keys the contract does not declare, which values have the wrong shape,
// and what a diagnostic about either says. Missing required keys are requiredkeys_test.go's
// subject, the two ways one mapping can name a thing twice are keyrepeats_test.go's, and the
// stage's error policy is shapepolicy_test.go's.

// stageF runs stages D, E and F over text and reports what the structural pass found, together
// with whether the shape it left is one the stages after it can read.
//
// The three stages run in the pipeline's own order because stage F is promised a document with
// one shape: aliases resolved, merge keys expanded and the operations list folded. A fixture that
// does not survive the earlier stages fails here rather than quietly asserting nothing about this
// one.
func stageF(t *testing.T, text string) (Errors, bool) {
	t.Helper()

	src, root, diags := stageE(t, text, corpusVariables)
	if len(diags) != 0 {
		t.Fatalf("the fixture does not reach stage F: %q\nsource:\n%s", messagesOf(diags), text)
	}
	return checkShape(src, root)
}

// pathsOf is the locator of every diagnostic, sorted, which is how a count-based claim says
// *which* one is missing rather than only that one is.
func pathsOf(diags Errors) []string {
	located := make([]string, 0, len(diags))
	for _, diag := range diags {
		located = append(located, diag.Path)
	}
	slices.Sort(located)
	return located
}

// tenUnknownKeys is AC #8's document: one key the contract declares nowhere, at each of the ten
// nesting levels the criterion enumerates. Everything else it writes is the minimal valid
// configuration, so every diagnostic it produces is one of the ten.
//
// The database URL carries the leak sentinel in its password, because this is the first fixture
// shape whose diagnostics quote lines near a declared secret, and an unknown key at
// `destination.signing` puts the caret one line from the secrets list.
const tenUnknownKeys = `version: 1
rootstray: 1
database:
  url: postgres://noty:` + leakSentinel + `1-HEAD-pw-` + leakSentinel + `1-TAIL@db.internal:5432/noty
  dbstray: 1
worker:
  workerstray: 1
retention:
  retentionstray: 1
defaults:
  retry:
    retrystray: 1
listeners:
  - name: order_paid
    table: public.orders
    listenerstray: 1
    operations:
      update:
        updatestray: 1
    payload:
      payloadstray: 1
    destination:
      url: https://hooks.example.test/order-paid
      destinationstray: 1
      signing:
        secrets: [` + leakSentinel + `2-HEAD-secret-` + leakSentinel + `2-TAIL]
        signingstray: 1
`

// TestAFreeFormMappingAcceptsAnyName is SC-9. A header mapping has no key vocabulary, so sweeping
// it into the unknown-key check would reject every header anyone writes -- and the exemption comes
// from the table rather than from the walk recognising the level.
func TestAFreeFormMappingAcceptsAnyName(t *testing.T) {
	document := "version: 1\ndatabase: {url: x}\n" +
		"defaults:\n  headers:\n    X-Anything: 1\n    Not-A-Declared-Key: 2\n" +
		"listeners:\n  - name: a\n    table: s.t\n    operations: [insert]\n" +
		"    destination:\n      url: https://h.test/x\n      headers:\n        X-Tenant: acme\n"

	if diags, _ := stageF(t, document); len(diags) != 0 {
		t.Errorf("%d diagnostics for arbitrary header names: %q", len(diags), messagesOf(diags))
	}
}

// The pieces a case builds its document out of. A case states the one thing it is about and
// inherits the rest, so every diagnostic it produces is its own.
//
// The four required listener keys are separate lines rather than one block because a case whose
// subject is a required key has to *drop* one of them, and a case whose subject is `operations` or
// `destination` has to *replace* it: two `operations` keys in one listener is a duplicate mapping
// key, which the parser refuses at stage B, and the document would never reach this stage at all.
const (
	requiredName        = "    name: order_paid\n"
	requiredTable       = "    table: public.orders\n"
	requiredOperations  = "    operations: [insert]\n"
	requiredDestination = "    destination:\n      url: https://hooks.example.test/order-paid\n"
)

// configAround is the minimal valid configuration with listener written as its one listener. The
// database URL is a literal rather than a reference so that a case's own document needs no
// environment beyond the corpus's.
//
// Line 1 is `version`, 2 `database`, 3 its url and 4 `listeners`, so a listener's own first key is
// always on line 5 -- which is what lets the anchoring cases count a column without restating the
// document.
func configAround(listener string) string {
	return "version: 1\n" +
		"database:\n  url: postgres://noty:pw@db.internal:5432/noty\n" +
		"listeners:\n" + listener
}

// aListenerOf is that configuration whose one listener writes exactly these lines.
//
// The `- ` opening the sequence item replaces the first line's indent rather than being written
// before it, so both are four characters wide: a case that drops the name still produces a
// well-formed list, and whichever key comes first still begins at rune 5.
func aListenerOf(lines ...string) string {
	return configAround("  - " + strings.TrimPrefix(strings.Join(lines, ""), "    "))
}

// listenerHolding is a complete listener plus the lines a case adds to it.
func listenerHolding(extra string) string {
	return aListenerOf(requiredName, requiredTable, requiredOperations, requiredDestination, extra)
}

// operationsOf is the same with the listener's operations block supplied by the case.
func operationsOf(block string) string {
	return aListenerOf(requiredName, requiredTable, block, requiredDestination)
}

// destinationOf is the same with the listener's destination block supplied by the case.
func destinationOf(block string) string {
	return aListenerOf(requiredName, requiredTable, requiredOperations, block)
}

package config

import (
	"strconv"
	"strings"
	"testing"
)

func TestWhenPrecheckRejectsForbiddenRowReferences(t *testing.T) {
	tests := []struct {
		name, operation, clause, statement, word string
	}{
		{"upper OLD under insert", "insert", `OLD.status = 'x'`, "INSERT", "OLD"},
		{"lower OLD under insert", "insert", `old.status = 'x'`, "INSERT", "OLD"},
		{"NEW under delete", "delete", `NEW.status = 'x'`, "DELETE", "NEW"},
		{"parenthesized OLD under insert", "insert", `(OLD).status = 'x'`, "INSERT", "OLD"},
		{"OLD after CR comment under insert", "insert", "-- ignored\rOLD.status = 'x'", "INSERT", "OLD"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			document := whenDocument(tc.operation, tc.clause)
			_, diags := stageH(t, document)
			if len(diags) != 1 {
				t.Fatalf("got %d diagnostics %q, want one", len(diags), messagesOf(diags))
			}
			got := diags[0]
			if got.Rule != R30 || got.Path != "listeners[0].operations."+tc.operation+".when" {
				t.Errorf("diagnostic = %+v, want R30 on the when value", got)
			}
			for _, phrase := range []string{
				tc.statement + " trigger's WHEN condition",
				"cannot reference " + tc.word + " values",
			} {
				if !strings.Contains(got.Msg, phrase) {
					t.Errorf("message %q does not quote %q", got.Msg, phrase)
				}
			}
			line, _ := occurrencePosition(t, document, "when:", 1)
			if got.Line != line {
				t.Errorf("diagnostic line = %d, want when line %d", got.Line, line)
			}
		})
	}
}

func TestWhenPrecheckStaysSilentOnAdversarialSQL(t *testing.T) {
	longTag := "$" + strings.Repeat("a", 63) + "$"
	tests := []struct {
		name, clause string
	}{
		{"single quoted", `status = 'OLD.status'`},
		{"dollar quoted", `$tag$OLD.status$tag$ = 'x'`},
		{"line comment", "-- OLD.status\nNEW.status > 0"},
		{"block comment", "/* OLD.status */ NEW.status > 0"},
		{"quoted identifier", `"OLD".status = 1`},
		{"longer word", "old_price > 0"},
		{"maximum byte tag", longTag + "OLD.status" + longTag},
		{"non ASCII tag", "$·$OLD.status$·$"},
		{"non ASCII word", "€old > 0"},
		{"continued escape string", "E'continued\\\\\n-- OLD.status\nstill string'"},
		{"selected lower case field", "NEW.old > 1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, diags := stageH(t, whenDocument("insert", tc.clause)); len(diags) != 0 {
				t.Fatalf("clause produced %q, want no false positive", messagesOf(diags))
			}
		})
	}

	if _, diags := stageH(t, whenDocument("delete", "OLD.new > 1")); len(diags) != 0 {
		t.Fatalf("selected NEW field under delete produced %q", messagesOf(diags))
	}
}

func whenDocument(operation, clause string) string {
	if strings.ContainsRune(clause, '\r') {
		operations := "    operations:\n      " + operation + ":\n        when: " + strconv.Quote(clause) + "\n"
		return aListenerOf(requiredName, requiredTable, operations, requiredDestination)
	}
	indented := strings.ReplaceAll(clause, "\n", "\n          ")
	operations := "    operations:\n      " + operation + ":\n        when: |-\n          " + indented + "\n"
	return aListenerOf(requiredName, requiredTable, operations, requiredDestination)
}

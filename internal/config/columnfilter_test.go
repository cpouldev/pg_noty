package config

import (
	"strings"
	"testing"
)

// R29's legality half: the three statements share one filter shape, and only one of them can be
// narrowed to a set of columns. Both sides of the rule are here, and so is the cascade it must not
// open -- a filter written in the wrong place is one mistake, not two.

// TestAColumnFilterIsLegalOnlyUnderUpdate is R29's legality half, and its accepting counterpart.
// The three statements share one filter shape, so `columns` beneath insert or delete is a key the
// contract declares in the wrong place rather than one it has never heard of -- which is why it is
// R29 and not R41.
func TestAColumnFilterIsLegalOnlyUnderUpdate(t *testing.T) {
	refused := map[string]string{
		"under insert": "    operations:\n      insert:\n        columns: [status]\n",
		"under delete": "    operations:\n      delete:\n        columns: [status]\n",
	}

	for name, body := range refused {
		t.Run(name, func(t *testing.T) {
			diags, _ := stageF(t, operationsOf(body))

			if len(diags) != 1 {
				t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
			}
			if diags[0].Rule != R29 {
				t.Errorf("Rule = %q, want R29", diags[0].Rule)
			}
			if !strings.Contains(diags[0].Msg, updateOperation) {
				t.Errorf("Msg = %q, which does not name the one statement a column filter narrows", diags[0].Msg)
			}
		})
	}

	t.Run("under update", func(t *testing.T) {
		body := "    operations:\n      update:\n        columns: [status]\n"
		if diags, _ := stageF(t, operationsOf(body)); len(diags) != 0 {
			t.Errorf("%d diagnostics for the one statement it is legal under: %q", len(diags), messagesOf(diags))
		}
	})
}

// TestAKeyWrittenInTheWrongPlaceIsNotAlsoJudgedOnItsContents keeps the cascade the step's Risks
// entry names from opening on this arm. An empty column filter under insert is one mistake -- the
// filter does not belong there at all -- and reporting its emptiness too would tell an author to
// fill in a list they have to delete.
func TestAKeyWrittenInTheWrongPlaceIsNotAlsoJudgedOnItsContents(t *testing.T) {
	diags, _ := stageF(t, operationsOf("    operations:\n      insert:\n        columns: []\n"))

	if len(diags) != 1 {
		t.Errorf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
	}
}

package config

import (
	"slices"
	"strings"
	"testing"
)

// R41 through the walk: which names the contract declares nowhere, where the diagnostic about one
// points, and which of the two hints it carries. The distance rule on its own is suggest_test.go's.

// TestTenUnknownKeysYieldTenDiagnostics is AC #8, the requirement ADR-1 exists for: a single
// aggregated "unknown field" error for the whole document does not satisfy it, and neither does a
// walk that stops at the first level it finds one.
//
// The expected locators are declared rather than counted, so a failure names the level that was
// missed instead of only saying the count was wrong.
func TestTenUnknownKeysYieldTenDiagnostics(t *testing.T) {
	want := []string{
		"database.dbstray",
		"defaults.retry.retrystray",
		"listeners[0].destination.destinationstray",
		"listeners[0].destination.signing.signingstray",
		"listeners[0].listenerstray",
		"listeners[0].operations.update.updatestray",
		"listeners[0].payload.payloadstray",
		"retention.retentionstray",
		"rootstray",
		"worker.workerstray",
	}

	diags, _ := stageF(t, tenUnknownKeys)

	if len(diags) != len(want) {
		t.Fatalf("%d diagnostics, want %d: %q", len(diags), len(want), messagesOf(diags))
	}
	if got := pathsOf(diags); !slices.Equal(got, want) {
		t.Errorf("locators\n got %q\nwant %q", got, want)
	}
	for _, diag := range diags {
		if diag.Rule != R41 {
			t.Errorf("%s is reported under %q, want R41", diag.Path, diag.Rule)
		}
		if diag.Line == 0 || diag.Col == 0 {
			t.Errorf("%s carries position %d:%d, want its own key token", diag.Path, diag.Line, diag.Col)
		}
	}
}

// TestEveryUnknownKeyIsAnchoredOnItsOwnKeyToken is the other half of AC #8: ten diagnostics that
// all pointed at the document root would satisfy the count and none of the requirement. Each
// caret must sit on the rune the offending key begins at, which for these ten is the first
// non-space character of the line the key is written on.
func TestEveryUnknownKeyIsAnchoredOnItsOwnKeyToken(t *testing.T) {
	diags, _ := stageF(t, tenUnknownKeys)
	lines := strings.Split(tenUnknownKeys, "\n")

	if len(diags) == 0 {
		t.Fatal("no diagnostics, so this would assert nothing")
	}
	for _, diag := range diags {
		line := lines[diag.Line-1]
		wantCol := len(line) - len(strings.TrimLeft(line, " ")) + 1

		if diag.Col != wantCol {
			t.Errorf("%s is at column %d, want %d, the first character of %q", diag.Path, diag.Col, wantCol, line)
		}
		if !strings.Contains(diag.Msg, strings.TrimSpace(strings.SplitN(line, ":", 2)[0])) {
			t.Errorf("%s says %q, which does not name the key written on line %d", diag.Path, diag.Msg, diag.Line)
		}
	}
}

// TestAnUnknownKeyIsSuggestedTheClosestDeclaredOne is AC #9 through the walk rather than through
// the distance function: the candidate list has to be the level's own, so `colums` under a payload
// reaches `columns` and a name resembling nothing reaches no hint at all.
func TestAnUnknownKeyIsSuggestedTheClosestDeclaredOne(t *testing.T) {
	tests := []struct {
		name     string
		document string
		wantHint string
	}{
		{
			name:     "a near miss is named",
			document: listenerHolding("    payload:\n      colums: [id, total]\n"),
			wantHint: `did you mean "columns"?`,
		},
		{
			name:     "a name resembling nothing is not",
			document: listenerHolding("    payload:\n      frobnicate: 1\n"),
			wantHint: "",
		},
		{
			name:     "a near miss at the operations level reaches that level's own names",
			document: operationsOf("    operations:\n      insrt: {}\n"),
			wantHint: `did you mean "insert"?`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags, _ := stageF(t, tc.document)

			if len(diags) != 1 {
				t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
			}
			if diags[0].Hint != tc.wantHint {
				t.Errorf("hint = %q, want %q", diags[0].Hint, tc.wantHint)
			}
		})
	}
}

// TestAnUnsupportedOperationIsRefusedByItsOwnRuleNamingTheThree is AC #16's `truncate` case. The
// operations level's vocabulary is a numbered rule of its own, so the diagnostic is R28's and
// names the three statements rather than R41's silence -- and it is exactly one diagnostic, with
// no cascade from descending into a filter the contract knows nothing about.
func TestAnUnsupportedOperationIsRefusedByItsOwnRuleNamingTheThree(t *testing.T) {
	diags, _ := stageF(t, operationsOf("    operations:\n      truncate: {}\n"))

	if len(diags) != 1 {
		t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
	}
	if diags[0].Rule != R28 {
		t.Errorf("Rule = %q, want R28", diags[0].Rule)
	}
	for _, statement := range []string{"insert", "update", "delete"} {
		if !strings.Contains(diags[0].Hint, statement) {
			t.Errorf("hint %q does not name %q as a supported operation", diags[0].Hint, statement)
		}
	}
}

// TestAMovedKeyIsNamedByItsMigrationHintRatherThanByItsNearestNeighbour is AC #10, and the case
// that makes the precedence load-bearing rather than incidental: `secret` is one edit from
// `secrets`, so a walk that asked for a suggestion first would answer `did you mean "secrets"?`
// and never tell its author that the key is now a list.
func TestAMovedKeyIsNamedByItsMigrationHintRatherThanByItsNearestNeighbour(t *testing.T) {
	tests := []struct {
		name     string
		document string
		wantHint string
	}{
		{
			name:     "destination.type",
			document: destinationOf("    destination:\n      url: https://h.test/x\n      type: http\n"),
			wantHint: "HTTP is the only destination kind",
		},
		{
			name:     "a listener-level when",
			document: listenerHolding("    when: \"NEW.status = 'paid'\"\n"),
			wantHint: "operations.<op>.when",
		},
		{
			name:     "a listener-level columns",
			document: listenerHolding("    columns: [status]\n"),
			wantHint: "operations.update.columns",
		},
		{
			name:     "signing.secret",
			document: destinationOf("    destination:\n      url: https://h.test/x\n      signing:\n        secret: s\n"),
			wantHint: "signing.secrets",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			diags, _ := stageF(t, tc.document)

			if len(diags) != 1 {
				t.Fatalf("%d diagnostics, want exactly 1: %q", len(diags), messagesOf(diags))
			}
			if !strings.Contains(diags[0].Hint, tc.wantHint) {
				t.Errorf("hint %q does not name %q", diags[0].Hint, tc.wantHint)
			}
			if strings.Contains(diags[0].Hint, "did you mean") {
				t.Errorf("hint %q is a suggestion; a moved key needs the location it moved to", diags[0].Hint)
			}
		})
	}
}

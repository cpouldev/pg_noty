package config

import (
	"slices"
	"strings"
	"testing"
)

func TestStageHSkipsAbsentNullAndUnreadableValues(t *testing.T) {
	t.Run("every absent wrapper", func(t *testing.T) {
		if got := validate(newSource("empty.yaml", nil), &rawConfig{}); len(got) != 0 {
			t.Errorf("an empty raw tree produced %q", messagesOf(got))
		}
	})

	t.Run("a required null is not an invented zero", func(t *testing.T) {
		text := replaceOnce(t, stageHExtensionSafeValue("  concurrency: 8\n"), "version: 1", "version: null")
		_, diags := stageH(t, text)
		if len(diags) != 0 {
			t.Errorf("the wrapper-unread null produced %q", messagesOf(diags))
		}
	})

	t.Run("an unreadable scalar is reported only by conversion", func(t *testing.T) {
		text := replaceOnce(t, stageHExtensionSafeValue("  concurrency: 8\n"), "concurrency: 8", "concurrency: nope")
		_, diags := stageH(t, text)
		if len(diags) != 1 || diags[0].Rule != RuleDecode {
			t.Errorf("got %q, want the one stage-G integer conversion refusal", messagesOf(diags))
		}
	})
}

func TestAbsentRequiredKeyAndAnotherMistakeProduceOneDiagnosticEach(t *testing.T) {
	text := replaceOnce(t, stageHExtensionSafeValue("  concurrency: 8\n"), "version: 1\n", "")
	text = replaceOnce(t, text, "batch_size: 100", "batch_size: 0")

	_, diags := stageH(t, text)
	if len(diags) != 2 {
		t.Fatalf("got %d diagnostics %q, want one required-key and one value diagnostic", len(diags), messagesOf(diags))
	}
	got := []RuleID{diags[0].Rule, diags[1].Rule}
	slices.Sort(got)
	want := []RuleID{R1, R7}
	if !slices.Equal(got, want) {
		t.Errorf("rules = %v, want %v", got, want)
	}
}

func TestDisabledListenerReceivesTheSameStaticValidation(t *testing.T) {
	base := stageHExtensionSafeValue("  concurrency: 8\n")
	broken := replaceOnce(t, base, "name: order_paid", "name: Order")
	broken = replaceOnce(t, broken, "mode: full", "mode: sideways")
	broken = replaceOnce(t, broken, "method: POST", "method: DELETE")

	rulesOf := func(text string) []RuleID {
		t.Helper()
		_, diags := stageH(t, text)
		rules := make([]RuleID, 0, len(diags))
		for _, diag := range diags {
			rules = append(rules, diag.Rule)
		}
		slices.Sort(rules)
		return rules
	}

	enabled := rulesOf(broken)
	disabled := rulesOf(replaceOnce(t, broken, "enabled: true", "enabled: false"))
	if want := []RuleID{R23, R31, R37}; !slices.Equal(enabled, want) {
		t.Fatalf("enabled listener rules = %v, want %v", enabled, want)
	}
	if !slices.Equal(disabled, enabled) {
		t.Errorf("disabled listener rules = %v, enabled = %v", disabled, enabled)
	}
}

func TestReservedSchemaAndSevenDaysOfferOneEditRepairs(t *testing.T) {
	base := stageHExtensionSafeValue("  concurrency: 8\n")
	tests := []struct {
		name, from, to, message, hint string
	}{
		{"schema", "schema: public", "schema: pg_noty", reservedSchemaPrefixReason, suggestedSchema},
		{"duration", "precreate: 48h", "precreate: 7d", "ns", "168h"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := stageH(t, replaceOnce(t, base, tc.from, tc.to))
			if len(diags) != 1 {
				t.Fatalf("got %q, want one diagnostic", messagesOf(diags))
			}
			if !strings.Contains(diags[0].Msg, tc.message) || !strings.Contains(diags[0].Hint, tc.hint) {
				t.Errorf("diagnostic = %+v, want message %q and hint %q", diags[0], tc.message, tc.hint)
			}
		})
	}
}

package config

import (
	"strings"
	"testing"
)

func TestW1CannotQuoteALiteralWhosePublicAnchorFeedsSigningSecrets(t *testing.T) {
	const secret = "PGNOTY-W1-PUBLIC-ANCHOR-SECRET"
	document := `version: 1
database:
  url: postgres://noty:pw@db.internal/noty
defaults:
  headers:
    X-Secret: &shared ` + secret + `
listeners:
- name: order_paid
  table: public.orders
  operations: [insert]
  destination:
    url: https://hooks.example.test/order-paid
    signing:
      secrets: [*shared]
`

	cfg, warnings, errs := Parse([]byte(document), "listeners.yaml", MapEnv(nil))
	if cfg == nil || len(errs) != 0 {
		t.Fatalf("fixture returned config=%v and errors=%+v", cfg != nil, errs)
	}
	if len(warnings) != 1 || warnings[0].Rule != W1 {
		t.Fatalf("warnings = %+v, want the literal-secret W1", warnings)
	}
	if rendered := warnings.Render([]byte(document)); strings.Contains(rendered, secret) {
		t.Fatalf("W1 quoted the public anchor definition that feeds signing.secrets:\n%s", rendered)
	}
}

// Alias reduction can move a scalar, a whole list, or a mapping into a sensitive path. A merge
// performs the same move entry by entry. Every marker brackets the definition line itself, which
// is the line later diagnostics inherit and therefore the extent the renderer must hide.
func TestSensitivityFollowsAliasesAndMergesBackToTheirPublicDefinitions(t *testing.T) {
	tests := []struct {
		name       string
		definition func(marked string) string
		use        string
	}{
		{
			name: "scalar alias used as a secret element",
			definition: func(marked string) string {
				return "    X-Shared: &shared " + marked + "\n"
			},
			use: "    signing:\n      secrets: [*shared]\n",
		},
		{
			name: "sequence alias used as the secrets list",
			definition: func(marked string) string {
				return "    X-Shared: &shared [" + marked + "]\n"
			},
			use: "    signing:\n      secrets: *shared\n",
		},
		{
			name: "mapping alias used as signing",
			definition: func(marked string) string {
				return "    X-Shared: &shared {secrets: [" + marked + "]}\n"
			},
			use: "    signing: *shared\n",
		},
		{
			name: "mapping merged into signing",
			definition: func(marked string) string {
				return "    X-Shared: &shared {secrets: [" + marked + "]}\n"
			},
			use: "    signing:\n      <<: *shared\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			watched := textMarkedAtBothEnds("ALIAS-DEFINITION", "literal-secret")
			document := aliasedSensitiveDocument(tc.definition(watched.text), tc.use)
			rendered := renderEveryLineOf(document)
			for _, marker := range watched.markers {
				if strings.Contains(rendered, marker) {
					t.Errorf("rendered output quoted definition marker %q:\n%s", marker, rendered)
				}
			}
		})
	}
}

func TestAliasesUsedOnlyByPublicPathsRemainVisibleAndUsable(t *testing.T) {
	const visible = "PUBLIC-ALIASED-HEADER"
	document := "version: 1\ndatabase:\n  url: postgres://db/noty\n" +
		"defaults:\n  headers:\n    X-First: &shared " + visible + "\n" +
		"    X-Second: *shared\nlisteners: []\n"

	cfg, warnings, errs := Parse([]byte(document), "listeners.yaml", MapEnv(nil))
	if cfg == nil || len(warnings) != 0 || len(errs) != 0 {
		t.Fatalf("public alias returned config=%v warnings=%+v errors=%+v", cfg != nil, warnings, errs)
	}
	rendered := renderEveryLineOf(document)
	for _, useful := range []string{visible, "*shared"} {
		if !strings.Contains(rendered, useful) {
			t.Errorf("public-only alias lost useful source text %q:\n%s", useful, rendered)
		}
	}
	if strings.Contains(rendered, redactionPlaceholder) {
		t.Errorf("a public-only alias was redacted:\n%s", rendered)
	}
}

func aliasedSensitiveDocument(definition, use string) string {
	return "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\n" +
		"defaults:\n  headers:\n" + definition +
		"listeners:\n- name: order_paid\n  table: public.orders\n  operations: [insert]\n" +
		"  destination:\n    url: https://hooks.example.test/order-paid\n" + use
}

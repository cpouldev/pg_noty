package config

import (
	"strings"
	"testing"
)

const (
	extentSecretHead = "PGNOTY-EXTENT-SECRET-HEAD"
	extentSecretTail = "PGNOTY-EXTENT-SECRET-TAIL"
)

var reducedAliasExtentCases = []struct {
	name     string
	document string
}{
	{
		name: "multiline scalar alias inside a later flow sequence",
		document: `shared: &shared "` + extentSecretHead + `
  ` + extentSecretTail + `"
public_before: KEEP-PUBLIC-BEFORE
public_middle: KEEP-PUBLIC-MIDDLE
listeners:
- destination:
    signing:
      secrets: [*shared]
public_after: KEEP-PUBLIC-AFTER
`,
	},
	{
		name: "multiline flow sequence alias",
		document: `shared: &shared ["` + extentSecretHead + `
` + extentSecretTail + `"] # PGNOTY-EXTENT-SECRET-COMMENT
public_before: KEEP-PUBLIC-BEFORE
public_middle: KEEP-PUBLIC-MIDDLE
listeners:
- destination:
    signing:
      secrets: *shared
public_after: KEEP-PUBLIC-AFTER
`,
	},
	{
		name: "sequence alias chain ending in a sensitive element",
		document: `shared: &shared "` + extentSecretHead + `
  ` + extentSecretTail + `"
relay: &relay [*shared]
public_before: KEEP-PUBLIC-BEFORE
public_middle: KEEP-PUBLIC-MIDDLE
listeners:
- destination:
    signing:
      secrets: *relay
public_after: KEEP-PUBLIC-AFTER
`,
	},
	{
		name: "merge and alias chain ending in signing",
		document: `shared: &shared "` + extentSecretHead + `
  ` + extentSecretTail + `"
base: &base {secrets: [*shared]}
relay: &relay {<<: *base}
public_before: KEEP-PUBLIC-BEFORE
public_middle: KEEP-PUBLIC-MIDDLE
listeners:
- destination:
    signing:
      <<: *relay
public_after: KEEP-PUBLIC-AFTER
`,
	},
}

func TestReducedAliasesInheritOnlyTheirDefinitionsExtent(t *testing.T) {
	for _, tc := range reducedAliasExtentCases {
		t.Run(tc.name, func(t *testing.T) {
			text := newSource("listeners.yaml", []byte(tc.document))
			root, diags := parseDocument(text)
			if !pathsAreResolvable(root, diags) {
				t.Fatalf("fixture does not reach path-aware redaction: %+v\n%s",
					diags, tc.document)
			}
			rendered := renderEveryLineOf(tc.document)
			for _, secret := range []string{
				extentSecretHead, extentSecretTail, "PGNOTY-EXTENT-SECRET-COMMENT",
			} {
				if strings.Contains(rendered, secret) {
					t.Errorf("%s survived alias redaction:\n%s", secret, rendered)
				}
			}
			for _, public := range []string{
				"KEEP-PUBLIC-BEFORE", "KEEP-PUBLIC-MIDDLE", "KEEP-PUBLIC-AFTER",
			} {
				if !strings.Contains(rendered, public) {
					t.Errorf("%s was blanked between definition and use:\n%s", public, rendered)
				}
			}
		})
	}
}

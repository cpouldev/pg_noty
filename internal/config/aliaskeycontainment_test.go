package config

import (
	"strings"
	"testing"
)

const aliasKeySecret = "PGNOTY-ALIAS-KEY-SECRET"

func TestSensitiveAliasKeyValueIsHiddenByPathAwareRedaction(t *testing.T) {
	document := sensitiveAliasKeyDocument("")
	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if !pathsAreResolvable(root, diags) {
		t.Fatalf("fixture does not reach path-aware redaction: %+v\n%s", diags, document)
	}
	assertAliasKeySecretIsHidden(t, document)
}

func TestSensitiveAliasKeyValueIsHiddenByFallbackRedaction(t *testing.T) {
	document := sensitiveAliasKeyDocument("broken: [\n")
	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if pathsAreResolvable(root, diags) {
		t.Fatal("fixture does not force the fallback branch")
	}
	assertAliasKeySecretIsHidden(t, document)
}

func TestPathAwareRedactionHidesAliasKeysInEveryMappingLayout(t *testing.T) {
	const anchor = "key_name: &sensitive_name secrets\n"
	for _, tc := range aliasKeyMappingLayouts {
		if !tc.pathAware {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			document := anchor + tc.body
			text := newSource("listeners.yaml", []byte(document))
			root, diags := parseDocument(text)
			if !pathsAreResolvable(root, diags) {
				t.Fatalf("fixture does not reach path-aware redaction: %+v\n%s", diags, document)
			}
			assertAliasKeySecretIsHidden(t, document)
		})
	}
}

func sensitiveAliasKeyDocument(tail string) string {
	return "key_name: &sensitive_name secrets\n" +
		"listeners:\n" +
		"- destination:\n" +
		"    signing:\n" +
		"      *sensitive_name : " + aliasKeySecret + "\n" +
		tail
}

func assertAliasKeySecretIsHidden(t *testing.T, document string) {
	t.Helper()
	if rendered := renderEveryLineOf(document); strings.Contains(rendered, aliasKeySecret) {
		t.Fatalf("an alias naming a sensitive key exposed its value:\n%s", rendered)
	}
}

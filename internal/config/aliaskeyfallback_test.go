package config

import (
	"strings"
	"testing"
)

var aliasKeyMappingLayouts = []struct {
	name      string
	body      string
	pathAware bool
}{
	{
		name: "leading block key", body: "*sensitive_name : " + aliasKeySecret + "\n",
		pathAware: true,
	},
	{
		name: "sequence item key", body: "unknown:\n- *sensitive_name : " + aliasKeySecret + "\n",
		pathAware: true,
	},
	{
		name: "nested flow mapping under unknown parent",
		body: "unknown: {nested: {*sensitive_name: " + aliasKeySecret + "}}\n",
	},
	{
		name: "flow mapping under declared signing",
		body: "listeners:\n- destination:\n    signing: {*sensitive_name: " +
			aliasKeySecret + "}\n",
	},
}

func TestFallbackRedactsAliasKeysInEveryMappingLayout(t *testing.T) {
	const anchor = "key_name: &sensitive_name secrets\n"
	for _, tc := range aliasKeyMappingLayouts {
		t.Run(tc.name, func(t *testing.T) {
			document := anchor + tc.body + "broken: [\n"
			assertFallbackDocumentHides(t, document, aliasKeySecret)
		})
	}
}

func TestFallbackRedactsEveryUnreadableKeyIntroducer(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{name: "unresolvable alias", line: "*missing : " + aliasKeySecret},
		{name: "anchor property", line: "&named ordinary : " + aliasKeySecret},
		{name: "tag property", line: "!!str ordinary : " + aliasKeySecret},
		{name: "explicit complex key", line: "? [ordinary, key] : " + aliasKeySecret},
		{
			name: "flow alias key",
			line: "ordinary: {nested: {*missing: " + aliasKeySecret + "}}",
		},
		{
			name: "flow explicit complex key",
			line: "ordinary: {nested: {? [ordinary, key] : " + aliasKeySecret + "}}",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertFallbackDocumentHides(t, tc.line+"\nbroken: [\n", aliasKeySecret)
		})
	}
}

func TestFallbackDoesNotMistakeAliasValuesOrOrdinaryKeysForUnreadableKeys(t *testing.T) {
	const public = "PGNOTY-PUBLIC-ALIAS-VALUE"
	document := "public_value: &public_value " + public + "\n" +
		"ordinary: *public_value\n" +
		"flow: {ordinary: *public_value, nested: {other: KEEP-PUBLIC-FLOW}}\n" +
		"sequence: [*public_value, ordinary]\n" +
		"broken: [\n"

	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if pathsAreResolvable(root, diags) {
		t.Fatal("fixture does not force fallback redaction")
	}
	rendered := renderEveryLineOf(document)
	for _, useful := range []string{
		public, "ordinary: *public_value", "other: KEEP-PUBLIC-FLOW",
		"sequence: [*public_value, ordinary]",
	} {
		if !strings.Contains(rendered, useful) {
			t.Errorf("fallback treated public alias value text as an unreadable key %q:\n%s",
				useful, rendered)
		}
	}
}

func assertFallbackDocumentHides(t *testing.T, document, marker string) {
	t.Helper()
	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if pathsAreResolvable(root, diags) {
		t.Fatal("fixture does not force fallback redaction")
	}
	if rendered := renderEveryLineOf(document); strings.Contains(rendered, marker) {
		t.Errorf("fallback exposed value beneath an unreadable key:\n%s", rendered)
	}
}

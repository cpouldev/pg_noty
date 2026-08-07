package config

import (
	"strings"
	"testing"
)

// Every value position may carry an anchor or tag. The schema walk must read the mapping,
// sequence, or sequence element beneath that property before it asks what the value holds.
func TestSchemaRedactionReadsEveryContainerPositionBeneathNodeProperties(t *testing.T) {
	const secret = "PGNOTY-WRAPPED-SENSITIVE-VALUE"
	tests := []struct {
		name     string
		document string
	}{
		{
			name:     "anchored child mapping",
			document: "database: &db {url: postgres://noty:" + secret + "@db.internal/noty}\n",
		},
		{
			name:     "tagged child mapping",
			document: "database: !!map {url: postgres://noty:" + secret + "@db.internal/noty}\n",
		},
		{
			name: "anchored nested mapping",
			document: "listeners:\n- destination:\n    signing: &signing\n" +
				"      secrets: [" + secret + "]\n",
		},
		{
			name: "tagged nested mapping",
			document: "listeners:\n- destination:\n    signing: !!map\n" +
				"      secrets: [" + secret + "]\n",
		},
		{
			name: "anchored sequence",
			document: "listeners:\n- destination:\n    signing:\n" +
				"      secrets: &rotation [" + secret + "]\n",
		},
		{
			name: "tagged sequence",
			document: "listeners:\n- destination:\n    signing:\n" +
				"      secrets: !!seq [" + secret + "]\n",
		},
		{
			name: "anchored sequence element",
			document: "listeners:\n- &listener\n  destination:\n    signing:\n" +
				"      secrets: [" + secret + "]\n",
		},
		{
			name: "tagged sequence element",
			document: "listeners:\n- !!map\n  destination:\n    signing:\n" +
				"      secrets: [" + secret + "]\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text := newSource("listeners.yaml", []byte(tc.document))
			root, diags := parseDocument(text)
			if !pathsAreResolvable(root, diags) {
				t.Fatalf("fixture does not reach the path-aware branch: %+v", diags)
			}

			found := sensitiveValues(text, schemaLevels[levelRoot], root)
			if !containsSensitiveText(found, secret) {
				t.Fatalf("schema walk did not find the wrapped secret in %q", tc.document)
			}
			if rendered := renderEveryLineOf(tc.document); strings.Contains(rendered, secret) {
				t.Errorf("rendered output quoted the wrapped secret:\n%s", rendered)
			}
		})
	}
}

func containsSensitiveText(values []sensitiveValue, text string) bool {
	for _, value := range values {
		if strings.Contains(value.text, text) {
			return true
		}
	}
	return false
}

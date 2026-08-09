package config

import (
	"strings"
	"testing"
)

const c5MaskedValue = "PGNOTY-C5-MASKED-VALUE"

var c5QuotedKeySpellings = []struct {
	name    string
	written string
}{
	{name: "escaped quote", written: `"u\"rl"`},
	{name: "escaped backslash", written: `"u\\rl"`},
	{name: "named escape", written: `"u\nrl"`},
	{name: "hex escape", written: `"u\x72l"`},
	{name: "short Unicode escape", written: `"u\u0072l"`},
	{name: "long Unicode escape", written: `"u\U00000072l"`},
	{name: "doubled single quote", written: `'u''rl'`},
}

var c5FlowLayouts = []struct {
	name  string
	open  string
	close string
}{
	{name: "direct mapping", open: "ordinary: {", close: "}"},
	{name: "later mapping entry", open: "ordinary: {public: value, ", close: "}"},
	{name: "nested mapping", open: "ordinary: {nested: {", close: "}}"},
	{name: "mapping in sequence", open: "ordinary: [{nested: {", close: "}}]"},
}

var c5QuotedFlowControls = []struct {
	name    string
	line    string
	visible string
}{
	{
		name: "simple double quoted key", line: `ordinary: {"public": C5-PUBLIC-DOUBLE}`,
		visible: "C5-PUBLIC-DOUBLE",
	},
	{
		name: "simple single quoted key", line: `ordinary: {'public': C5-PUBLIC-SINGLE}`,
		visible: "C5-PUBLIC-SINGLE",
	},
	{
		name:    "punctuation in double quoted key",
		line:    `ordinary: {"public:#,:[]{}": C5-PUBLIC-DOUBLE-PUNCTUATION}`,
		visible: "C5-PUBLIC-DOUBLE-PUNCTUATION",
	},
	{
		name:    "punctuation in single quoted key",
		line:    `ordinary: {'public:#,:[]{}': C5-PUBLIC-SINGLE-PUNCTUATION}`,
		visible: "C5-PUBLIC-SINGLE-PUNCTUATION",
	},
	{
		name:    "escaped double quoted value",
		line:    `ordinary: {public: "C5-PUBLIC-DOUBLE-VALUE-\x41:#,{}[]"}`,
		visible: "C5-PUBLIC-DOUBLE-VALUE",
	},
	{
		name:    "doubled single quoted value",
		line:    `ordinary: {public: 'C5-PUBLIC-SINGLE-VALUE-''-:#,{}[]'}`,
		visible: "C5-PUBLIC-SINGLE-VALUE",
	},
	{
		name:    "quoted key text in comment",
		line:    `ordinary: {"public": C5-PUBLIC-COMMENT} # {"u\x72l": ignored}`,
		visible: "C5-PUBLIC-COMMENT",
	},
}

func c5FlowLine(layout struct {
	name  string
	open  string
	close string
}, key, value string) string {
	return layout.open + key + ": " + value + layout.close
}

func TestC5UnreadableQuotedFlowKeyGrid(t *testing.T) {
	for _, key := range c5QuotedKeySpellings {
		for _, layout := range c5FlowLayouts {
			t.Run(key.name+"/"+layout.name, func(t *testing.T) {
				line := c5FlowLine(layout, key.written, c5MaskedValue)
				if !containsUnreadableFlowKey(line) {
					t.Errorf("containsUnreadableFlowKey(%q) = false, want true", line)
				}
			})
		}
	}
}

func TestC5FallbackMasksUnreadableQuotedFlowKeyGrid(t *testing.T) {
	for _, key := range c5QuotedKeySpellings {
		for _, layout := range c5FlowLayouts {
			t.Run(key.name+"/"+layout.name, func(t *testing.T) {
				line := c5FlowLine(layout, key.written, c5MaskedValue)
				assertFallbackDocumentHides(t, line+"\nbroken: [\n", c5MaskedValue)
			})
		}
	}
}

func TestC5QuotedFlowControlClassification(t *testing.T) {
	for _, tc := range c5QuotedFlowControls {
		t.Run(tc.name, func(t *testing.T) {
			if containsUnreadableFlowKey(tc.line) {
				t.Errorf("containsUnreadableFlowKey(%q) = true, want false", tc.line)
			}
		})
	}
}

func TestC5QuotedFlowControlsRemainVisible(t *testing.T) {
	for _, tc := range c5QuotedFlowControls {
		t.Run(tc.name, func(t *testing.T) {
			rendered := renderEveryLineOf(tc.line + "\nbroken: [\n")
			if !strings.Contains(rendered, tc.visible) {
				t.Errorf("fallback removed public text %q:\n%s", tc.visible, rendered)
			}
		})
	}
}

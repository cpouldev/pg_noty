package config

import (
	"strings"
	"testing"
)

func TestSensitiveFlowMappingContinuesAtTheAnchorIndent(t *testing.T) {
	const secret = "PGNOTY-SAME-INDENT-FLOW-SECRET"
	document := "listeners:\n" +
		"- destination:\n" +
		"    signing:\n" +
		"      \"secrets\": {value: \"closed\",\n" +
		"      other: " + secret + "}#tail\n"

	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if !pathsAreResolvable(root, diags) {
		t.Fatalf("fixture does not reach path-aware redaction: %+v\n%s", diags, document)
	}
	if rendered := renderEveryLineOf(document); strings.Contains(rendered, secret) {
		t.Fatalf("same-indent flow continuation exposed a sensitive value:\n%s", rendered)
	}
}

func TestEveryMultilineSensitiveFlowContainerIsRedactedThroughItsClosingLine(t *testing.T) {
	keys := []string{"secrets", `"secrets"`}
	breaks := []struct {
		name string
		text string
	}{
		{name: "line feed", text: "\n"},
		{name: "lone carriage return", text: "\r"},
	}
	indents := []struct {
		name string
		text string
	}{
		{name: "less indentation", text: "  "},
		{name: "same indentation", text: "      "},
		{name: "greater indentation", text: "        "},
	}
	containers := []struct {
		name  string
		lines []string
	}{
		{
			name: "nested flow mapping",
			lines: []string{
				`{PGNOTY-FLOW-FIRST: visible,`,
				`PGNOTY-FLOW-MIDDLE: [visible,`,
				`PGNOTY-FLOW-LAST]}#PGNOTY-FLOW-TAIL`,
			},
		},
		{
			name: "nested flow sequence",
			lines: []string{
				`[PGNOTY-FLOW-FIRST,`,
				`{middle: PGNOTY-FLOW-MIDDLE,`,
				`last: PGNOTY-FLOW-LAST}] # PGNOTY-FLOW-COMMENT`,
			},
		},
	}

	for _, key := range keys {
		for _, lineBreak := range breaks {
			for _, indent := range indents {
				for _, container := range containers {
					name := key + "/" + lineBreak.name + "/" + indent.name + "/" + container.name
					t.Run(name, func(t *testing.T) {
						document := sensitiveFlowDocument(key, lineBreak.text, indent.text, container.lines)
						assertSensitiveFlowDocumentIsContained(t, document, lineBreak.text == "\n")
					})
				}
			}
		}
	}
}

func sensitiveFlowDocument(key, lineBreak, continuationIndent string, lines []string) string {
	return "listeners:" + lineBreak +
		"- destination:" + lineBreak +
		"    signing:" + lineBreak +
		"      " + key + ": " + lines[0] + lineBreak +
		continuationIndent + lines[1] + lineBreak +
		continuationIndent + lines[2] + lineBreak +
		"public_boundary: KEEP-FLOW-BOUNDARY" + lineBreak
}

func assertSensitiveFlowDocumentIsContained(t *testing.T, document string, preservesBoundary bool) {
	t.Helper()
	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if !pathsAreResolvable(root, diags) {
		t.Fatalf("fixture does not reach path-aware redaction: %+v\n%q", diags, document)
	}

	rendered := renderEveryLineOf(document)
	for _, marker := range []string{
		"PGNOTY-FLOW-FIRST",
		"PGNOTY-FLOW-MIDDLE",
		"PGNOTY-FLOW-LAST",
		"PGNOTY-FLOW-TAIL",
		"PGNOTY-FLOW-COMMENT",
	} {
		if strings.Contains(rendered, marker) {
			t.Errorf("%s survived a sensitive flow container:\n%s", marker, rendered)
		}
	}
	if preservesBoundary && !strings.Contains(rendered, "KEEP-FLOW-BOUNDARY") {
		t.Errorf("redaction passed the flow container's closing line:\n%s", rendered)
	}
}

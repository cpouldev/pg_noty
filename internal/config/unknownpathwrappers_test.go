package config

import (
	"strings"
	"testing"
)

func TestWrappedUnknownParentStillRecursesToASensitiveLeaf(t *testing.T) {
	const secret = "PGNOTY-WRAPPED-UNKNOWN-PARENT"
	document := "unknown: &outer !!map {nested: !!map " +
		"{url: postgres://u:" + secret + "@h/db}}\n"

	assertWrappedUnknownDocumentIsContained(t, document, secret)
}

func TestUndeclaredParentRecursionReadsEveryContainerBeneathProperties(t *testing.T) {
	const secret = "PGNOTY-UNKNOWN-RECURSION-SECRET"
	leaves := []struct {
		name string
		text string
	}{
		{name: "url", text: "url: postgres://u:" + secret + "@h/db"},
		{name: "listen_url", text: "listen_url: postgres://u:" + secret + "@h/db"},
		{name: "secrets", text: "secrets: " + secret},
	}
	layouts := []struct {
		name string
		tag  string
		body func(string, string) string
	}{
		{
			name: "nested mapping", tag: "!!map",
			body: func(wrapper, leaf string) string {
				return "{nested: " + wrapper + "{" + leaf + "}}"
			},
		},
		{
			name: "nested sequence", tag: "!!seq",
			body: func(wrapper, leaf string) string {
				return "{nested: " + wrapper + "[{" + leaf + "}]}"
			},
		},
		{
			name: "wrapped sequence element", tag: "!!map",
			body: func(wrapper, leaf string) string {
				return "{nested: [" + wrapper + "{" + leaf + "}]}"
			},
		},
	}
	properties := []struct {
		name   string
		prefix func(string) string
	}{
		{name: "anchor", prefix: func(string) string { return "&wrapped " }},
		{name: "tag", prefix: func(tag string) string { return tag + " " }},
		{name: "anchor and tag", prefix: func(tag string) string {
			return "&wrapped " + tag + " "
		}},
	}

	for _, leaf := range leaves {
		for _, layout := range layouts {
			for _, property := range properties {
				name := leaf.name + "/" + layout.name + "/" + property.name
				t.Run(name, func(t *testing.T) {
					value := layout.body(property.prefix(layout.tag), leaf.text)
					assertWrappedUnknownDocumentIsContained(
						t, "unknown: "+value+"\n", secret)
				})
			}
		}
	}
}

func assertWrappedUnknownDocumentIsContained(t *testing.T, document, secret string) {
	t.Helper()
	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if !pathsAreResolvable(root, diags) {
		t.Fatalf("fixture does not reach path-aware redaction: %+v\n%s", diags, document)
	}
	found := sensitiveValues(text, schemaLevels[levelRoot], root)
	if !containsSensitiveText(found, secret) {
		t.Fatalf("unknown-parent recursion did not find the wrapped leaf:\n%s", document)
	}
	if rendered := renderEveryLineOf(document); strings.Contains(rendered, secret) {
		t.Fatalf("wrapped unknown-parent recursion exposed a credential:\n%s", rendered)
	}
}

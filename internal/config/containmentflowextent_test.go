package config

import (
	"strings"
	"testing"
)

type flowExtentVariant struct {
	name  string
	first string
	last  string
}

var flowExtentVariants = []flowExtentVariant{
	{
		name:  "double quoted scalar with an escaped quote",
		first: `["PGNOTY-FLOW-HEAD \" escaped`,
		last:  `PGNOTY-FLOW-TAIL", "PGNOTY-FLOW-CLOSING"] # PGNOTY-FLOW-AFTER-CLOSE PGNOTY-FLOW-COMMENT`,
	},
	{
		name:  "single quoted scalar with a doubled quote",
		first: `['PGNOTY-FLOW-HEAD '' doubled`,
		last:  `PGNOTY-FLOW-TAIL', 'PGNOTY-FLOW-CLOSING'] # PGNOTY-FLOW-AFTER-CLOSE PGNOTY-FLOW-COMMENT`,
	},
	{
		name:  "nested sequence with multiple elements",
		first: `[["PGNOTY-FLOW-HEAD`,
		last:  `PGNOTY-FLOW-TAIL", "PGNOTY-FLOW-MIDDLE"], "PGNOTY-FLOW-CLOSING"] # PGNOTY-FLOW-COMMENT`,
	},
	{
		name:  "nested mapping with multiple elements",
		first: `[{first: "PGNOTY-FLOW-HEAD`,
		last:  `PGNOTY-FLOW-TAIL", second: "PGNOTY-FLOW-MIDDLE"}, "PGNOTY-FLOW-CLOSING"] # PGNOTY-FLOW-COMMENT`,
	},
}

var flowExtentIndents = []struct {
	name string
	text string
}{
	{name: "column one", text: ""},
	{name: "lower indentation", text: "  "},
	{name: "anchor indentation", text: "      "},
	{name: "greater indentation", text: "        "},
}

func TestDeclaredSensitiveFlowChildrenInheritTheirContainerClosingLine(t *testing.T) {
	for _, key := range keySpellings {
		for _, indent := range flowExtentIndents {
			name := key.name + "/" + indent.name
			t.Run(name, func(t *testing.T) {
				document := declaredFlowExtentDocument(
					key, "\n", indent.text, flowExtentVariants[0])
				assertFlowExtentIsContained(t, document, true)
			})
		}
	}
}

func TestEverySplitSensitiveFlowChildUsesItsLocalEnclosingExtent(t *testing.T) {
	targets := []struct {
		name  string
		build func(keySpelling, string, string, flowExtentVariant) string
	}{
		{name: "declared path", build: declaredFlowExtentDocument},
		{name: "wrapped unknown parent", build: unknownFlowExtentDocument},
	}
	breaks := []struct {
		name string
		text string
	}{
		{name: "line feed", text: "\n"},
		{name: "lone carriage return", text: "\r"},
	}

	for _, target := range targets {
		for _, key := range keySpellings {
			for _, lineBreak := range breaks {
				for _, indent := range flowExtentIndents {
					for _, variant := range flowExtentVariants {
						if target.name == "declared path" && lineBreak.text == "\n" &&
							variant.name == flowExtentVariants[0].name {
							continue
						}
						name := target.name + "/" + key.name + "/" + lineBreak.name +
							"/" + indent.name + "/" + variant.name
						t.Run(name, func(t *testing.T) {
							assertFlowExtentIsContained(t,
								target.build(key, lineBreak.text, indent.text, variant),
								lineBreak.text == "\n")
						})
					}
				}
			}
		}
	}
}

func declaredFlowExtentDocument(
	key keySpelling,
	lineBreak, indent string,
	variant flowExtentVariant,
) string {
	return "listeners:" + lineBreak +
		"- destination:" + lineBreak +
		"    signing:" + lineBreak +
		"      " + key.write("secrets") + " " + variant.first + lineBreak +
		indent + variant.last + lineBreak +
		"public_boundary: KEEP-FLOW-BOUNDARY" + lineBreak
}

func unknownFlowExtentDocument(
	key keySpelling,
	lineBreak, indent string,
	variant flowExtentVariant,
) string {
	last := strings.Replace(variant.last, "] #", "]}} #", 1)
	return "unknown: &outer !!map {nested: &inner !!map {" +
		key.write("secrets") + " " + variant.first + lineBreak +
		indent + last + lineBreak +
		"public_boundary: KEEP-FLOW-BOUNDARY" + lineBreak
}

func assertFlowExtentIsContained(t *testing.T, document string, preservesBoundary bool) {
	t.Helper()
	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if !pathsAreResolvable(root, diags) {
		t.Fatalf("fixture does not reach path-aware redaction: %+v\n%q", diags, document)
	}

	rendered := renderEveryLineOf(document)
	for _, marker := range []string{
		"PGNOTY-FLOW-HEAD",
		"PGNOTY-FLOW-TAIL",
		"PGNOTY-FLOW-MIDDLE",
		"PGNOTY-FLOW-CLOSING",
		"PGNOTY-FLOW-AFTER-CLOSE",
		"PGNOTY-FLOW-COMMENT",
	} {
		if strings.Contains(rendered, marker) {
			t.Errorf("%s survived its sensitive container:\n%s", marker, rendered)
		}
	}
	if preservesBoundary && !strings.Contains(rendered, "KEEP-FLOW-BOUNDARY") {
		t.Errorf("redaction passed the sensitive container's closing line:\n%s", rendered)
	}
}

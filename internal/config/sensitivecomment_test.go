package config

import (
	"strings"
	"testing"
)

func TestFallbackCommentCannotCloseTheSensitiveFlowSequenceItAppearsIn(t *testing.T) {
	const secret = "PGNOTY-REVIEW-COMMENT-FLOW-SECRET"
	document := "listeners:\n- destination:\n    signing:\n" +
		"      secrets: [first # ] comment\n" +
		"      other: " + secret + "\n"

	if rendered := renderEveryLineOf(document); strings.Contains(rendered, secret) {
		t.Fatalf("fallback quoted text after a flow closer inside a comment:\n%s", rendered)
	}
}

func TestSensitiveContinuationRecognisesCommentsOnlyAtYAMLSeparation(t *testing.T) {
	tests := []struct {
		name      string
		afterKey  string
		wantOpen  bool
		wantQuote byte
	}{
		{name: "sequence closer in a separated comment", afterKey: " [first # ] comment", wantOpen: true},
		{name: "mapping closer in a separated comment", afterKey: " {value: first # } comment", wantOpen: true},
		{name: "tab separates a comment", afterKey: " [first\t# ] comment", wantOpen: true},
		{name: "adjacent hash is plain data in a sequence", afterKey: " [first#]", wantOpen: false},
		{name: "adjacent hash is plain data in a mapping", afterKey: " {value: first#}", wantOpen: false},
		{name: "hash immediately after a key colon is data", afterKey: "#[", wantOpen: true},
		{name: "space separates a comment after a key colon", afterKey: " #[", wantOpen: false},
		{name: "tab separates a comment after a key colon", afterKey: "\t#[", wantOpen: false},
		{name: "hash inside double quotes is data", afterKey: ` ["first # ] data"]`, wantOpen: false},
		{name: "hash inside single quotes is data", afterKey: ` ['first # ] data']`, wantOpen: false},
		{name: "hash after an escaped double quote is data", afterKey: ` ["first \" # ] data"]`, wantOpen: false},
		{name: "hash after a doubled single quote is data", afterKey: ` ['first '' # ] data']`, wantOpen: false},
		{
			name:     "escaped quote leaves the hash inside an open double quote",
			afterKey: ` ["first \" # ] comment`,
			wantOpen: true, wantQuote: '"',
		},
		{
			name:     "doubled quote leaves the hash inside an open single quote",
			afterKey: ` ['first '' # ] comment`,
			wantOpen: true, wantQuote: '\'',
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line := "secrets:" + tc.afterKey
			got := sensitiveContinuationAfter(line, byteOffset(len("secrets:")))
			if got.open() != tc.wantOpen {
				t.Errorf("continuation %+v open = %t, want %t for %q",
					got, got.open(), tc.wantOpen, tc.afterKey)
			}
			if got.quote != tc.wantQuote {
				t.Errorf("quote = %q, want %q for %q", got.quote, tc.wantQuote, tc.afterKey)
			}
		})
	}
}

func TestCommentedFlowClosersCannotExposeLaterPhysicalOrParserLineTails(t *testing.T) {
	containers := []struct {
		name string
		line string
	}{
		{name: "flow sequence", line: "[first # ] comment"},
		{name: "flow mapping", line: "{value: first # } comment"},
	}
	breaks := []struct {
		name string
		text string
	}{
		{name: "line-feed parser line", text: "\n"},
		{name: "lone-carriage-return physical tail", text: "\r"},
	}

	for _, container := range containers {
		for _, ending := range breaks {
			for _, spelling := range keySpellings {
				name := container.name + ", " + ending.name + ", " + spelling.name
				t.Run(name, func(t *testing.T) {
					watched := textMarkedAtBothEnds("COMMENT-PHYSICAL-TAIL", "ordinary tail")
					document := signingSecrets("      " + spelling.write("secrets") + " " + container.line +
						ending.text + "      other: " + watched.text + "\n")
					text := newSource("listeners.yaml", []byte(document))
					root, diags := parseDocument(text)
					if pathsAreResolvable(root, diags) {
						t.Fatal("fixture unexpectedly avoids the fallback branch")
					}
					rendered := renderEveryLineOf(document)
					for _, marker := range watched.markers {
						if strings.Contains(rendered, marker) {
							t.Errorf("rendered output quoted tail marker %q:\n%s", marker, rendered)
						}
					}
				})
			}
		}
	}
}

func TestFallbackCarriesRedactionAcrossEverySegmentOfOnePhysicalLine(t *testing.T) {
	const secret = "PGNOTY-AFTER-RESET-PHYSICAL-TAIL"
	document := signingSecrets(
		"      secrets: {value: first # } comment\n" +
			"      other: closes-the-flow}\n" +
			"outside-the-block\r" + secret + "\n",
	)
	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if pathsAreResolvable(root, diags) {
		t.Fatal("fixture unexpectedly avoids the fallback branch")
	}

	if rendered := renderEveryLineOf(document); strings.Contains(rendered, secret) {
		t.Fatalf("fallback stopped redacting between segments of one physical line:\n%s", rendered)
	}
}

func TestFallbackKeepsUnclassifiedLinesCoveredUntilAReadableKey(t *testing.T) {
	const secret = "PGNOTY-AFTER-UNCLASSIFIED-LINE"
	document := signingSecrets(
		"      secrets: [first # ] comment\n" +
			"      other: closes-the-flow]\n" +
			"first-unclassified-line\n" +
			secret + "\n" +
			"version: 1\n",
	)
	rendered := renderEveryLineOf(document)
	if strings.Contains(rendered, secret) {
		t.Fatalf("fallback treated one unclassified line as a safe boundary:\n%s", rendered)
	}
	if !strings.Contains(rendered, "version: 1") {
		t.Fatalf("fallback passed the next readable key:\n%s", rendered)
	}
}

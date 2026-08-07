package config

import (
	"slices"
	"strings"
	"testing"
)

// These rows close the fallback's continuation state over all three value containers, both
// YAML quote escape rules, and comments that contain apparent flow closers.
//
// The four rows written out below plant their secret *raw*, so its own line breaks decide where its
// later lines land, and a generated tail holding the container's own closer ends the flow they rely
// on -- after which the remainder sits at column one and reads as a key of its own. Each therefore
// indents its continuation past the sensitive key (Note 24). The rows built by quotedFallbackLayout
// need no such care and deliberately do not have it: they escape the quote they open, so no secret
// can close it, and every line until the file ends belongs to the value whatever its indentation.
func fallbackSensitiveContinuationLayouts() []secretLayout {
	return []secretLayout{
		quotedFallbackLayout("a direct single-quoted scalar with a doubled quote", "", '\'', "doubled '' quote "),
		quotedFallbackLayout("a flow sequence with an escaped double quote", "[", '"', `escaped \" quote `),
		quotedFallbackLayout("a flow sequence with a doubled single quote", "[", '\'', "doubled '' quote "),
		quotedFallbackLayout("a flow mapping with an escaped double quote", "{value: ", '"', `escaped \" quote `),
		quotedFallbackLayout("a flow mapping with a doubled single quote", "{value: ", '\'', "doubled '' quote "),
		{
			name:         "an unclosed flow sequence after a closed quoted element",
			fallbackOnly: true,
			body: func(secret string, key keySpelling) string {
				entry := "      other: " + watchedTailSlot + secret
				return signingSecrets("      " + key.write("secrets") + " [\"closed\",\n" +
					indentedContinuationLines(entry, "        ") + "\n")
			},
			watchedTail: textMarkedAtBothEnds("FLOW-SEQUENCE-TAIL", "continued "),
		},
		{
			name:         "an unclosed flow mapping after a closed quoted value",
			fallbackOnly: true,
			body: func(secret string, key keySpelling) string {
				entry := "      other: " + watchedTailSlot + secret
				return signingSecrets("      " + key.write("secrets") + " {value: \"closed\",\n" +
					indentedContinuationLines(entry, "        ") + "\n")
			},
			watchedTail: textMarkedAtBothEnds("FLOW-MAPPING-TAIL", "continued "),
		},
		{
			name:         "a flow sequence whose apparent closer is inside a comment",
			fallbackOnly: true,
			body: func(secret string, key keySpelling) string {
				entry := "      other: " + watchedTailSlot + secret
				return signingSecrets("      " + key.write("secrets") + " [first # ] comment\n" +
					indentedContinuationLines(entry, "        ") + "\n")
			},
			watchedTail: textMarkedAtBothEnds("COMMENT-SEQUENCE-TAIL", "continued "),
		},
		{
			name:         "a flow mapping whose apparent closer is inside a comment",
			fallbackOnly: true,
			body: func(secret string, key keySpelling) string {
				entry := "      other: " + watchedTailSlot + secret
				return signingSecrets("      " + key.write("secrets") + " {value: first # } comment\n" +
					indentedContinuationLines(entry, "        ") + "\n")
			},
			watchedTail: textMarkedAtBothEnds("COMMENT-MAPPING-TAIL", "continued "),
		},
	}
}

func quotedFallbackLayout(name, flow string, quote byte, escaped string) secretLayout {
	return secretLayout{
		name:         name,
		fallbackOnly: true,
		body: func(secret string, key keySpelling) string {
			return signingSecrets("      " + key.write("secrets") + " " + flow + string(quote) + escaped + "\n" +
				"      other: " + watchedTailSlot + quoteContinuation(secret, quote) + "\n")
		},
		watchedTail: textMarkedAtBothEnds(strings.ToUpper(strings.ReplaceAll(flowName(flow), " ", "-")),
			"continued "),
	}
}

func quoteContinuation(text string, quote byte) string {
	if quote == '\'' {
		return strings.ReplaceAll(text, "'", "''")
	}
	return quoteWithoutClosing(text)
}

func flowName(flow string) string {
	switch {
	case strings.HasPrefix(flow, "["):
		return "flow sequence tail"
	case strings.HasPrefix(flow, "{"):
		return "flow mapping tail"
	default:
		return "direct quote tail"
	}
}

func TestFallbackRedactsAKeyShapedContinuationInsideAnUnclosedFlowSequenceQuote(t *testing.T) {
	const secret = "PGNOTY-UNCLOSED-FLOW-SEQUENCE-SECRET"
	document := "listeners:\n- destination:\n    signing:\n      secrets: [\"\n" +
		"      other: " + secret + "\n"

	if rendered := renderEveryLineOf(document); strings.Contains(rendered, secret) {
		t.Fatalf("fallback quoted the flow sequence's sensitive continuation:\n%s", rendered)
	}
}

// The watched physical tail is part of the generated oracle, not merely text planted in a
// layout. Removing its markers from documentsHiding must make this assertion fail.
func TestEveryWatchedPhysicalTailContributesEveryMarkerToEveryPlantedDocument(t *testing.T) {
	watched := 0
	for _, layout := range secretLayouts {
		if layout.watchedTail.text == "" {
			continue
		}
		watched++
		for _, spelling := range keySpellings {
			for _, planted := range documentsHiding(secretMarkedOnEveryLine("secret"), layout, spelling) {
				for _, marker := range layout.watchedTail.markers {
					if !slices.Contains(planted.markers, marker) {
						t.Errorf("%s, %s, %s omits watched-tail marker %q",
							layout.name, spelling.name, planted.branch, marker)
					}
				}
			}
		}
	}
	if watched == 0 {
		t.Fatal("no layout watches a physical tail, so the oracle assertion is vacuous")
	}
}

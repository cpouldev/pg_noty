package config

import (
	"strconv"
	"strings"
)

// This file is the oracle the containment tests detect a leak with, and nothing else.
//
// A search for a planted marker can only see the part of the input that marker covers. A sentinel
// written once at the front of a secret therefore watches its first line and nothing else, so a
// leak of any later line of a multi-line value is indistinguishable from a pass. That is the
// mechanism by which three separate under-redaction classes survived three reviews and millions of
// generated executions: the generator reached each class, and the oracle could not see it.
//
// Every line of every planted secret is therefore marked at both ends, and the oracle searches for
// every one of those markers. The marking is purely additive -- stripping the markers gives the
// secret's bytes back exactly -- so no byte class the grid enumerates is lost to it, which
// TestMarkingASecretOnlyAddsToIt asserts.

// leakSentinel opens every marker these tests plant, so rendered output is searched for text that
// cannot occur by coincidence. Matching on the secret itself would let a one-character secret pass
// by accident.
const leakSentinel = "PGNOTY-SENTINEL-"

// markedSecret is a secret whose every line is watched, together with the markers no rendered
// output may hold. text is what gets planted; finding any entry of markers in rendered output is a
// leak.
type markedSecret struct {
	text    string
	markers []string
}

// textMarkedAtBothEnds watches an ordinary, non-secret extent that must nevertheless disappear
// with the sensitive physical line sharing it. The CR-split tail and the continuation following an
// unclosed quote are the two such extents in the containment grid.
func textMarkedAtBothEnds(label, text string) markedSecret {
	head := leakSentinel + "WATCH-" + label + "-HEAD"
	foot := leakSentinel + "WATCH-" + label + "-TAIL"
	return markedSecret{
		text:    head + text + foot,
		markers: []string{head, foot},
	}
}

// secretMarkedOnEveryLine is a secret made of tail, with every one of its lines bracketed by its
// own pair of markers.
//
// Both ends of each line are marked because redaction truncates a line in both directions: blanking
// keeps a line's head and drops its tail, while replacing a value keeps whatever sits past the
// value's own length. A marker at one end only would leave one of those two partial leaks
// invisible.
//
// The breaks tail was written with are kept exactly as they are *by this function*, because which break
// a secret holds is one of the classes secretTails enumerates. That was not enough on its own: nine of
// the ten layouts then wrote the marked secret through strconv.Quote, which escapes a raw carriage
// return to two characters, so only 16 of 160 planted documents held one and the class was unreachable
// everywhere but the block-scalar layout. Preserving a byte here and escaping it downstream leaves the
// claim true and the coverage absent, which is why the layouts now quote through yamlQuoted.
func secretMarkedOnEveryLine(tail string) markedSecret {
	var text strings.Builder
	var markers []string

	for number, line := range linesWithTheirBreaks(tail) {
		exempt, content := splitLeadingCommentIndicator(line.content)
		head, foot := leakMarker(number+1, "HEAD"), leakMarker(number+1, "TAIL")

		text.WriteString(exempt + head + content + foot + line.ending)
		markers = append(markers, head, foot)
	}
	return markedSecret{text: text.String(), markers: markers}
}

// leakMarker names one end of one line of a planted secret, so a failure says which line leaked and
// which of its ends survived. A separator follows the number, so no marker is a substring of
// another and a report cannot name line 1 for a leak of line 11
// (TestNoMarkerIsASubstringOfAnother).
func leakMarker(line int, end string) string {
	return leakSentinel + strconv.Itoa(line) + "-" + end
}

// splitLeadingCommentIndicator splits a line into the comment indicator it may open with and the
// rest of it, so a marker is written *after* the indicator rather than in front of it.
//
// It is the one token looksLikeAComment (lineshape.go) matches a line by *prefix*, so a marker
// written before it would move the generated line out of the class the redactor branches on -- the
// value would no longer be the value the code decides about. The two document markers need no such
// care: holdsNoBytesToHide matches them only as a whole line, which leaves no room for a marker at
// all, so TestTheFallbackBlanksEveryShapeAValueCanBeWrittenIn covers that shape as a row of its own.
func splitLeadingCommentIndicator(line string) (string, string) {
	if rest, found := strings.CutPrefix(line, commentIndicator); found {
		return commentIndicator, rest
	}
	return "", line
}

// plantedLine is one line of a planted secret: its text, and the break that ended it -- empty for
// the last line.
type plantedLine struct {
	content string
	ending  string
}

// lineBreaks are the byte sequences that end a line, longest first so a CRLF pair is read as one
// break rather than two. A lone carriage return is among them because the parser reads it as a
// break, and the oracle must watch both sides of that disagreement with splitLines.
var lineBreaks = []string{"\r\n", "\n", "\r"}

// linesWithTheirBreaks splits text into its lines, each keeping the break it ended with.
func linesWithTheirBreaks(text string) []plantedLine {
	var lines []plantedLine

	for rest := text; ; {
		at, ending := firstLineBreakIn(rest)
		if ending == "" {
			return append(lines, plantedLine{content: rest})
		}

		lines = append(lines, plantedLine{content: rest[:at], ending: ending})
		rest = rest[at+len(ending):]
	}
}

// firstLineBreakIn is where the first line break of text begins and which break it is, or an empty
// break when text holds none. The scan steps rune by rune, so it cannot report an offset inside a
// multi-byte character.
func firstLineBreakIn(text string) (int, string) {
	for at := range text {
		for _, ending := range lineBreaks {
			if strings.HasPrefix(text[at:], ending) {
				return at, ending
			}
		}
	}
	return 0, ""
}

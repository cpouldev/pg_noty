package config

import (
	"strings"
	"testing"
)

// This file generates the containment grid's inner-line opener dimension: a quote or a flow
// container opened on a line the fallback blanks *by position* rather than on the sensitive key's
// own line.
//
// It exists because two of those arms blanked their line and then handed the next line the lexical
// state they had been given rather than the state their own line left, so a quote opened there was
// never tracked. Once the block's indentation rule ended at a dedent, the scalar's key-shaped
// continuation was read as a public sibling key and rendered verbatim. Every layout wrote its opener
// on the sensitive key's own line, where the default arm did seed the state, so the class was
// unreachable from every seed and every mutation of one.

// lexicalOpener is one token a line can leave open, with how to write a secret after it so the
// secret's own bytes cannot close it again.
type lexicalOpener struct {
	name   string
	opens  string
	closes string
	write  func(string) string
}

// openersALineCanLeaveOpen is one row per byte sensitiveContinuation is still open after reading.
// The set is measured rather than copied from that switch:
// TestEveryByteThatLeavesTheContinuationOpenHasARow offers all 256 of them to the production state
// machine, so a fifth opener fails there by name before it can fail as a branch no containment run
// reaches.
var openersALineCanLeaveOpen = []lexicalOpener{
	{name: "a double-quoted scalar", opens: `"`, closes: `"`, write: quoteWithoutClosing},
	{name: "a single-quoted scalar", opens: "'", closes: "'", write: singleQuoteWithoutClosing},
	{name: "a flow sequence", opens: "[", closes: "]", write: yamlQuoted},
	{name: "a flow mapping", opens: "{", closes: "}", write: yamlQuoted},
}

// openerPosition is where inside a sensitive block the opening line sits -- one row per arm of
// redactBySensitiveKeyName that blanks a line it did not itself open the block with. Both arms had
// to be taught to carry the state, so generating only one of them would leave the other's carry
// unfalsifiable.
type openerPosition struct {
	name              string
	opensOnASplitTail bool
	write             func(secrets, opened string) string
}

var positionsAnOpenerCanBeWrittenAt = []openerPosition{
	{
		name: "an inner line of the block",
		write: func(secrets, opened string) string {
			return "      " + secrets + "\n        - " + opened + "\n"
		},
	},
	{
		name: "a carriage-return split tail", opensOnASplitTail: true,
		write: func(secrets, opened string) string {
			return "      " + secrets + " first\r        - " + opened + "\n"
		},
	},
}

// innerLineOpenerLayouts write the opener at each of those positions, and the secret again on a line
// dedented to column one whose bytes read as an ordinary public key. Only the lexical state the
// opening line leaves can cover that line: indentation puts it outside the block, and its own name
// matches no sensitive one, so the fallback would otherwise render it as written.
func innerLineOpenerLayouts() []secretLayout {
	layouts := make([]secretLayout, 0, len(openersALineCanLeaveOpen)*len(positionsAnOpenerCanBeWrittenAt))
	for _, opener := range openersALineCanLeaveOpen {
		for _, at := range positionsAnOpenerCanBeWrittenAt {
			layouts = append(layouts, secretLayout{
				name:         "a secret behind " + opener.name + " opened on " + at.name,
				fallbackOnly: true,
				body: func(secret string, key keySpelling) string {
					written := innerLineSecret(secret, opener)
					return signingSecrets(
						at.write(key.write("secrets"), opener.opens+written)+
							"tail: "+written+opener.closes+"\n") + unparseableTail
				},
			})
		}
	}
	return layouts
}

// innerLineSecret is the secret as one of these rows writes it: folded onto one line, then escaped
// for the opener it sits inside.
func innerLineSecret(secret string, opener lexicalOpener) string {
	return opener.write(onOneLine(secret))
}

// TestEveryByteThatLeavesTheContinuationOpenHasARow closes the opener table over the production
// state machine by measurement rather than against a number copied beside it. Both directions are
// asserted: a byte the machine stays open after must be written by a row, and a byte a row opens
// with must leave the machine open, or that row's dedented line is covered by nothing.
func TestEveryByteThatLeavesTheContinuationOpenHasARow(t *testing.T) {
	written := make(map[string]string, len(openersALineCanLeaveOpen))
	for _, opener := range openersALineCanLeaveOpen {
		written[opener.opens] = opener.name
	}

	for code := range 256 {
		token := string([]byte{byte(code)})
		leavesOpen := sensitiveContinuationAfter(token, 0).open()
		named, hasRow := written[token]

		if leavesOpen && !hasRow {
			t.Errorf("sensitiveContinuation stays open after %q and no layout opens with it, so the "+
				"arm it introduces is a branch no containment run can reach", token)
		}
		if !leavesOpen && hasRow {
			t.Errorf("the %s row opens with %q, after which sensitiveContinuation is closed, so its "+
				"dedented line is covered by nothing and the row plants no shape", named, token)
		}
	}

	want := len(openersALineCanLeaveOpen) * len(positionsAnOpenerCanBeWrittenAt)
	if got := len(innerLineOpenerLayouts()); got != want {
		t.Errorf("%d inner-line layouts for %d openers at %d positions", got,
			len(openersALineCanLeaveOpen), len(positionsAnOpenerCanBeWrittenAt))
	}
}

// TestEachOpenerPositionReachesTheArmItIsNamedFor measures that the two positions are two arms and
// not one. The split-tail row's opening line has to be the tail of the physical line above it, and
// the inner-line row's has to not be; were they the same, one arm's carry would be exercised by
// nothing.
func TestEachOpenerPositionReachesTheArmItIsNamedFor(t *testing.T) {
	for _, at := range positionsAnOpenerCanBeWrittenAt {
		t.Run(at.name, func(t *testing.T) {
			document := "version: 1\n" + signingSecrets(
				at.write(keySpellings[0].write("secrets"), `"`+shapeProbeSecret))
			text := newSource("listeners.yaml", []byte(document))

			if got := holdsASplitTail(text); got != at.opensOnASplitTail {
				t.Errorf("the document holds a carriage-return split tail = %t, and the row is named "+
					"for %t:\n%q", got, at.opensOnASplitTail, document)
			}
		})
	}
}

func holdsASplitTail(text *source) bool {
	for number := firstLine; number <= len(text.lines); number++ {
		if text.continuesTheLineAbove(number) {
			return true
		}
	}
	return false
}

// TestEveryInnerLineOpenerSurvivesEverySeedSecret asserts the precondition each row declares, over
// every secret the grid can plant in it: after the opening line -- the one the fallback blanks by
// position alone -- the lexical state is still open. A generated secret that closed its own opener
// would leave the row asserting nothing about the class it is named for.
func TestEveryInnerLineOpenerSurvivesEverySeedSecret(t *testing.T) {
	for _, tail := range seedTails() {
		secret := secretMarkedOnEveryLine(tail).text
		for _, opener := range openersALineCanLeaveOpen {
			opening := "        - " + opener.opens + innerLineSecret(secret, opener)
			if !sensitiveContinuationAfter(opening, 0).open() {
				t.Errorf("%s closes on the line it is opened on, for the secret made of %q",
					opener.name, tail)
			}
		}
	}
}

// TestTheFallbackRedactsAQuoteOpenedOnAnInnerLineOfASensitiveBlock is the leak this family was
// written for, kept independently reproducible beside its rows in the grid: the quote is opened on
// the block's second line, and the line carrying the rest of the secret is dedented to column one
// and shaped exactly like a public sibling key.
func TestTheFallbackRedactsAQuoteOpenedOnAnInnerLineOfASensitiveBlock(t *testing.T) {
	document := "version: 1\n" + signingSecrets(
		"      secrets:\n"+
			"        - \""+leakSentinel+"OPENED-INSIDE\n"+
			"tail: "+leakSentinel+"DEDENTED-CONTINUATION\"\n") + unparseableTail

	if pathsAreResolvable(parseDocument(newSource("", []byte(document)))) {
		t.Fatalf("the document parses, so it cannot show what the fallback does:\n%s", document)
	}
	if rendered := renderEveryLineOf(document); strings.Contains(rendered, leakSentinel) {
		t.Errorf("the fallback quoted a scalar opened on an inner line of a sensitive block:\n%s",
			rendered)
	}
}

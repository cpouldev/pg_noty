package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/parser"
)

// This file runs the second channel by which text reaches rendered output: a diagnostic's own Msg
// and Hint, which are written beside the quoted lines and are not quoted from them.
//
// CK-11 says no rendered-output path bypasses redact.go. Its test scanned which functions may
// receive the configuration *bytes*, which this channel never does -- the parser receives them, and
// hands back a message it composed out of them. So the checklist item read as satisfied while
// `mapping key "<a key written inside a signing secret>" already defined` was rendered verbatim
// under a line quoted as `[redacted]`.

// TestADiagnosticMessageQuotingAWithheldWordIsContained is the reproduction. The key is duplicated
// inside `signing.secrets`, whose whole value the table declares secret, so the redactor blanks both
// lines and the parser names the key it blanked.
func TestADiagnosticMessageQuotingAWithheldWordIsContained(t *testing.T) {
	document := "version: 1\n" + signingSecrets(
		"      secrets:\n        "+leakSentinel+"DUP: a\n        "+leakSentinel+"DUP: b\n")

	text := newSource("listeners.yaml", []byte(document))
	_, diags := parseDocument(text)
	if len(diags) != 1 {
		t.Fatalf("the document produced %d diagnostics, want the duplicate key's one: %+v", len(diags), diags)
	}
	if !strings.Contains(diags[0].Msg, leakSentinel) {
		t.Fatalf("the parser no longer names the duplicated key (%q), so this document no longer "+
			"reaches the channel it was written for", diags[0].Msg)
	}

	rendered := diags.Render([]byte(document))
	if strings.Contains(rendered, leakSentinel) {
		t.Errorf("a diagnostic message rendered a word the redactor withheld:\n%s", rendered)
	}
	// The condition still has to be legible, or containment has been bought by saying nothing.
	if !strings.Contains(rendered, "already defined") {
		t.Errorf("the message no longer names the condition:\n%s", rendered)
	}
}

// TestContainingDiagnosticTextLeavesWordsTheRedactorKept is the other side of the guard: it must not
// fire on text the document never withheld (.claude/rules/test-both-sides-of-an-exclusion-guard.md).
func TestContainingDiagnosticTextLeavesWordsTheRedactorKept(t *testing.T) {
	lines := quotableLines{}.withholding([]string{"S3CRET-VALUE", "a"})

	for _, row := range []struct {
		name, text, want string
	}{
		{name: "no withheld word", text: "did not find expected key", want: "did not find expected key"},
		{name: "the whole word", text: `key "S3CRET-VALUE" is`, want: `key "[redacted]" is`},
		{
			// The withheld `a` is written in every one of these and standing alone in none.
			name: "a one-letter word inside longer ones",
			text: "already defined at a later line",
			want: "already defined at [redacted] later line",
		},
		{
			name: "a withheld word inside a longer one",
			text: "S3CRET-VALUES were found",
			want: "S3CRET-VALUES were found",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := lines.contained(row.text); got != row.want {
				t.Errorf("contained(%q) = %q, want %q", row.text, got, row.want)
			}
		})
	}
}

// TestAnUnrecognisedParseFailureIsReportedWithoutItsText reaches the fail-open default directly,
// because nothing this library returns does (.claude/rules/assert-the-refusal-not-only-its-absence.md).
func TestAnUnrecognisedParseFailureIsReportedWithoutItsText(t *testing.T) {
	planted := errors.New("parse failed near:\n  secrets: " + leakSentinel + "PLANTED")

	rule, message, at := classifyParseError(planted)

	if rule != RuleSyntax {
		t.Errorf("rule = %q, want %q; an unreadable failure is still a syntax failure", rule, RuleSyntax)
	}
	if at != nil {
		t.Errorf("an error carrying no token was positioned at %+v", at)
	}
	if message != unrecognisedParseFailureMessage {
		t.Errorf("message = %q, want %q; an unrecognised failure's own text is rendered without a "+
			"position, which is the case no redaction reads any source for",
			message, unrecognisedParseFailureMessage)
	}
}

// TestEveryParseFailureThisLibraryProducesIsASyntaxError is what makes the refusal above affordable:
// the arm is unreachable for every document the invalid corpus holds
// (.claude/rules/refuse-an-unknown-shape-unconditionally.md).
func TestEveryParseFailureThisLibraryProducesIsASyntaxError(t *testing.T) {
	failures := 0
	for _, path := range fixturesIn(t, invalidCorpus) {
		_, err := parser.ParseBytes(readFixtureBytes(t, path), parser.ParseComments)
		if err == nil {
			continue
		}
		failures++

		var syntax *yaml.SyntaxError
		if !errors.As(err, &syntax) {
			t.Errorf("%s fails to parse with a %T, which reaches the refusal above; that arm reports "+
				"no detail, so this shape now needs a reading of its own", filepath.Base(path), err)
		}
	}

	// Without this the claim is satisfied by a corpus in which nothing fails to parse at all
	// (.claude/rules/count-the-population-a-vacuity-guard-guards.md).
	if failures == 0 {
		t.Fatal("no fixture in the invalid corpus fails to parse, so this proves nothing about " +
			"which error shapes the library produces")
	}
}

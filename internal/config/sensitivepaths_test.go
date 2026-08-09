package config

import (
	"strings"
	"testing"
)

// This file covers D3's path-aware branch: while the document parses, which values are secret
// is decided by where they sit, so the same key name can be a secret in one place and public
// in another.

// TestPathAwareRedactionKeepsTheDestinationUrlAndBlanksTheSecrets is the asymmetry end to
// end, through the public renderer. The diagnostic is anchored on the last line so that
// every line above it is inside some gutter window across the run.
func TestPathAwareRedactionKeepsTheDestinationUrlAndBlanksTheSecrets(t *testing.T) {
	if !strings.Contains(secretBearingSource, leakSentinel) {
		t.Fatal("the fixture plants no sentinel, so searching the output for one would pass vacuously")
	}

	rendered := renderEveryLineOf(secretBearingSource)

	if strings.Contains(rendered, leakSentinel) {
		t.Errorf("rendered output quotes a secret:\n%s", rendered)
	}
	for _, want := range []string{
		// The URL's shape survives: scheme, user, host, port and database name.
		"postgres://noty:[redacted]@db.internal:5432/noty",
		// The destination URL has no credentials, so AC #20 can render it verbatim.
		"url: ftp://host/x",
		// A literal secret goes wholesale, while the reference beside it stays legible.
		"- [redacted]",
		"- ${SIGNING_SECRET}",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered output does not contain %q:\n%s", want, rendered)
		}
	}
}

// TestASensitiveKeyIsFoundWhateverSpellingItIsWrittenIn is the path-aware branch reading key
// text through the one answer stage D reads it through (keyTextOf, valueposition.go).
//
// A key's own token is only its introducer -- `?` for an explicit key, `!!str` for a tagged
// one, `&name` for an anchored one -- so a walk that read the token matched none of the three,
// and the password beneath a key spelled any of those ways reached rendered output in the
// clear (Implementation Note 2). The plain row is what keeps the other three honest: it fails
// too if the walk stops finding `url` at all.
func TestASensitiveKeyIsFoundWhateverSpellingItIsWrittenIn(t *testing.T) {
	const password = "s3cret-under-a-spelling"
	const connection = "postgres://noty:" + password + "@db.internal/noty"

	spellings := []struct {
		name        string
		document    string
		wantInPlace bool
	}{
		{name: "a plain key", document: "database:\n  url: " + connection + "\n", wantInPlace: true},
		{name: "an explicit key", document: "database:\n  ? url\n  : " + connection + "\n", wantInPlace: true},
		{name: "an anchored key", document: "database:\n  &shared url: " + connection + "\n", wantInPlace: true},
		{
			// Measured, not assumed: for `!!str url: value` the parser reports the value at
			// the rune *before* its first character -- column 13 of `  !!str url: postgres…`,
			// which is the space -- while the other three spellings report it exactly. D3's
			// prefix test therefore fails and the line is blanked from there, which is the
			// direction this file's contract allows: over-redaction is acceptable, under-
			// redaction never is. Before the walk read key text through keyTextOf this
			// spelling was not found at all and the password rendered in the clear, so the
			// blanking is the improvement rather than the defect.
			name:        "a tagged key",
			document:    "database:\n  !!str url: " + connection + "\n",
			wantInPlace: false,
		},
	}

	for _, tc := range spellings {
		t.Run(tc.name, func(t *testing.T) {
			rendered := renderEveryLineOf(tc.document)

			if strings.Contains(rendered, password) {
				t.Errorf("the password under this spelling reached rendered output:\n%s", rendered)
			}
			// Only the password goes where the position allows it: a connection string blanked
			// wholesale makes its own diagnostic useless, which is the asymmetry urlPassword
			// exists for.
			inPlace := strings.Contains(rendered, "postgres://noty:[redacted]@db.internal/noty")
			if inPlace != tc.wantInPlace {
				t.Errorf("redacted in place = %v, want %v:\n%s", inPlace, tc.wantInPlace, rendered)
			}
		})
	}
}

// TestASecretSpanningLinesDoesNotLeak is the named guard for the detector Implementation Note 1
// records, which nothing else names.
//
// node.String() is a scalar's source text except for the two shapes that span lines: a block literal
// returns its indicator plus its content, and a multi-line double-quoted scalar is folded onto one
// line. So redactValue tests strings.HasPrefix against the line and, when it fails, blanks the rest
// of that line plus every line indented under it. That test is the detector for a multi-line value
// rather than a sanity check -- without it, replaceRunes replaces the folded form's rune count and
// every line of the secret but the first is rendered untouched. Each of those lines is therefore
// watched separately, because a marker on the first line alone cannot tell that apart from a pass.
func TestASecretSpanningLinesDoesNotLeak(t *testing.T) {
	secret := secretMarkedOnEveryLine("first line\nsecond line\nthird line")
	source := "listeners:\n" +
		"- destination:\n" +
		"    signing:\n" +
		"      secrets:\n" +
		"      - |\n" +
		indentedLines(secret.text, 10) +
		"  name: order_paid\n"

	text := newSource("listeners.yaml", []byte(source))
	root, diags := parseDocument(text)
	if !pathsAreResolvable(root, diags) {
		t.Fatalf("the document does not take the path-aware branch, where the detector lives:\n%s", source)
	}
	if len(secret.markers) != 6 {
		t.Fatalf("a three-line secret reports %d markers, want 6: the extent is not fully watched",
			len(secret.markers))
	}

	rendered := renderEveryLineOf(source)

	for _, marker := range secret.markers {
		if strings.Contains(rendered, marker) {
			t.Errorf("%s survived a block scalar spanning three lines:\n%s", marker, rendered)
		}
	}
	// The other side of the guard: the extent must end where the block does, or "no leak" would
	// be satisfied by blanking the rest of the document.
	if want := "name: order_paid"; !strings.Contains(rendered, want) {
		t.Errorf("the line after the block scalar was blanked too, so its extent was not found:\n%s", rendered)
	}
}

// TestTheSecondConnectionStringKeepsEverythingButItsPassword is database.listen_url's half of CK-7.
// The table declares two connection strings sensitive at the same extent and only one carried the
// surviving-parts assertion, so a regression that blanked this one wholesale would have passed while
// still satisfying "the secret is gone" -- and left its diagnostic unable to say what it rejected.
func TestTheSecondConnectionStringKeepsEverythingButItsPassword(t *testing.T) {
	source := "database:\n" +
		"  listen_url: postgres://noty:" + leakSentinel + "listen@replica.internal:5432/noty\n"

	rendered := renderEveryLineOf(source)

	if strings.Contains(rendered, leakSentinel) {
		t.Errorf("rendered output quotes the listen URL's password:\n%s", rendered)
	}
	if want := "postgres://noty:[redacted]@replica.internal:5432/noty"; !strings.Contains(rendered, want) {
		t.Errorf("rendered output does not contain %q, so the diagnostic cannot name what it rejected:\n%s",
			want, rendered)
	}
}

// TestTwoSecretsOnOneLineAreBothReplaced is what makes the replacement order matter: a flow
// sequence puts both entries on one line, so replacing the first shortens the line by eleven
// runes and moves the second. Replacing right to left leaves each column where it was derived;
// left to right lands the second replacement in the wrong place, which this expectation catches.
//
// The wanted line is derived by substituting each entry's text in place, so the comma and both
// brackets survive around them. It is also the invariant replaceRunes rests on: two spans on
// one line never overlap, so no replacement can shorten the line before another one's own end.
func TestTwoSecretsOnOneLineAreBothReplaced(t *testing.T) {
	source := "listeners:\n" +
		"- destination:\n" +
		"    signing:\n" +
		"      secrets: [" + leakSentinel + "first, " + leakSentinel + "second]\n"

	rendered := renderEveryLineOf(source)

	if strings.Contains(rendered, leakSentinel) {
		t.Errorf("rendered output quotes a secret:\n%s", rendered)
	}
	if want := "secrets: [[redacted], [redacted]]"; !strings.Contains(rendered, want) {
		t.Errorf("rendered output does not contain %q:\n%s", want, rendered)
	}
}

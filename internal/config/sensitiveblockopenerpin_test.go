package config

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// This file pins the two library facts the opener-gap derivation rests on. A comment recording a
// measurement does not fail when the library changes, so each is an assertion instead. The
// containment claims built on them are in sensitiveblockopener_test.go and the grid they run is in
// containmentopenergap_test.go.

// TestGoccyAnchorsAValueOnTheFirstTokenOfItsOwnText pins the two facts the gap derivation rests on,
// per shape: which line the parser anchors a value on, and that the token before that value's first
// token is the `:` its key was written with. Both are measured here, because the derivation reads
// the parser's own token stream to find the delimiter that opens the block.
//
// The tree is prepared exactly as the redactor prepares it -- parsed *and normalized* -- because
// that is the tree both readers of the extent see. The distinction is not academic: stage E removes
// an anchor property from the tree, so an anchored block sequence is anchored on its `&` before
// normalization and on its first item marker after, and only the second answer puts the property's
// own line in the gap.
func TestGoccyAnchorsAValueOnTheFirstTokenOfItsOwnText(t *testing.T) {
	for _, shape := range []struct {
		name string
		body string
		// wantAnchor and wantOpener are derived from the document below, whose `secrets:` key is
		// always line 6 of it: version 1, listeners, the item, destination, signing, secrets.
		wantAnchor int
		wantOpener int
	}{
		{name: "a comment above a block sequence",
			body:       "      secrets:\n        # note\n        - one\n",
			wantAnchor: 8, wantOpener: 6},
		{name: "a comment above a block mapping",
			body:       "      secrets:\n        # note\n        a: one\n",
			wantAnchor: 8, wantOpener: 6},
		{name: "an anchor property above a block sequence",
			body:       "      secrets:\n        &name\n        - one\n",
			wantAnchor: 8, wantOpener: 6},
		{name: "a flow sequence written beside its key",
			body:       "      secrets: [one, two]\n",
			wantAnchor: 6, wantOpener: 6},
		{name: "a block sequence at its key's own indentation",
			body:       "      secrets:\n      # note\n      - one\n",
			wantAnchor: 8, wantOpener: 6},
		// The tag becomes the value, so the value's own text begins on the tag's line and there is
		// no gap left. This row is why a `!!tag` is not a cell of the opener-gap grid.
		{name: "a tag above a block sequence",
			body:       "      secrets:\n        !!seq\n        - one\n",
			wantAnchor: 7, wantOpener: 6},
	} {
		t.Run(shape.name, func(t *testing.T) {
			text, value := theValueDeclaredSecretIn(t, shape.body)

			if got := writtenStartOf(text, value).line; got != shape.wantAnchor {
				t.Errorf("the parser anchors the value on line %d, want %d; the gap is measured "+
					"from this answer", got, shape.wantAnchor)
			}
			got, found := blockOpenerLineOf(text, value)
			if !found {
				t.Fatalf("no opener found for a value written under a key, so the gap above it is " +
					"outside every extent again")
			}
			if got != shape.wantOpener {
				t.Errorf("the block opening this value is reported on line %d, want %d",
					got, shape.wantOpener)
			}
		})
	}
}

// theValueDeclaredSecretIn is the single node one document writes at a declared `secrets` locator,
// with the source it was read from.
//
// The document is written around body at a fixed depth, so a row can count its own line numbers:
// `version: 1` is line 1 and signingSecrets writes four more, which puts body's first line at line
// 6 of every document this builds.
//
// The tree is prepared exactly as the redactor prepares it -- parsed *and normalized* -- for the
// reason TestGoccyAnchorsAValueOnTheFirstTokenOfItsOwnText gives above: stage E removes an anchor
// property, and only the post-normalization answer puts that property's own line in the gap. Both
// this file and sensitiveextentseparation_test.go measure through it, so neither can drift into
// measuring a differently prepared tree.
func theValueDeclaredSecretIn(t *testing.T, body string) (*source, ast.Node) {
	t.Helper()

	document := "version: 1\n" + signingSecrets(body)
	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if !pathsAreResolvable(root, diags) {
		t.Fatalf("the document does not parse:\n%s", document)
	}
	_ = normalize(text, root)

	values := declaredSecretsValuesOf(root)
	if len(values) != 1 {
		t.Fatalf("%d nodes written at a declared secrets locator, want exactly one:\n%s",
			len(values), document)
	}
	return text, values[0]
}

// TestAValueUnderNoKeyAtAllReportsNoBlockOpener reaches the branch no sensitive value reaches today
// and asserts what it answers, rather than proving only that nothing arrives there. The document
// root is introduced by no key, so there is no colon to reach back to and the extent may not be
// widened one line: were the answer a line instead, every rune above the root -- a licence header,
// a `---` -- would be blanked as part of the first value found under it.
func TestAValueUnderNoKeyAtAllReportsNoBlockOpener(t *testing.T) {
	text := newSource("listeners.yaml", []byte("version: 1\ndatabase:\n  url: postgres://h/n\n"))
	root, diags := parseDocument(text)
	if !pathsAreResolvable(root, diags) {
		t.Fatalf("the document does not parse, so it does not reach the branch this pins")
	}

	if line, found := blockOpenerLineOf(text, root); found {
		t.Errorf("the document root reports an opener on line %d; no key introduced it", line)
	}
	written := writtenStartReachingItsBlockOpener(text, root)
	if written.reachesBackTo != written.line {
		t.Errorf("the extent reaches back to line %d from line %d, want no widening at all",
			written.reachesBackTo, written.line)
	}
}

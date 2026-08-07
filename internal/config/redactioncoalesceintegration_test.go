package config

import (
	"strings"
	"testing"
)

// This is the renderer-level oracle for coalescing. `redactioncoalesce_test.go` calls
// coalescedSensitiveValues directly, so deleting the production call to it in sensitivepaths.go
// left that test green; these cases drive the whole render path instead, which is the only place
// the call exists.
//
// Each fixture reaches **one** written extent through **two** schema paths whose declarations
// differ in strength: `database.url` loses only its password (urlPassword) while an entry of
// `destination.signing.secrets` goes wholesale (entireValue). One replacement must land, at the
// stricter of the two.
//
// Every fixture writes the shared value **quoted**, and that is load-bearing rather than a style
// choice. In flow context an unquoted `postgres://reader:PW@host/db` is not one scalar: YAML reads
// the `:` as a mapping indicator, so goccy v1.19.2 returns a nested single-pair mapping keyed
// `postgres://reader`, the anchor binds to that key, and the two paths then reach two *different*
// extents -- one written value, two nodes, two replacements, and a case that no longer tests
// coalescing at all. assertOneExtentTwoStrengths asserts the precondition rather than trusting
// it, so a fixture that splits again fails by name instead of as a bare count mismatch.
// The password is padded, and that is load-bearing in the same way the quoting is. Without
// coalescing both declarations replace the same extent, and the second replaceRunes counts the
// *original* value's runes into a copy the first has already resized -- so it over-runs the value's
// end by exactly the length the first replacement changed. With an eleven-rune password that
// over-run is one rune, which lands on the closing quote and destroys nothing an assertion watches:
// both "partial path first" rows passed against a build with the coalescing call deleted, so the
// name claimed an order the row could not test. Padding the password makes the over-run long enough
// to reach the tail the caret is derived against, and all four rows now fail without the mechanism.
const (
	sharedExtentValue      = `"postgres://reader:SHAREDEXTENTPASSWORD-PADDED-SO-THE-OVERRUN-BITES@SHAREDEXTENTHOST.example/db"`
	sharedExtentTailMarker = "SHAREDEXTENTTAILMARKER"
	sharedExtentMessage    = "shared extent geometry"
)

// sharedExtentRendererCases vary the two dimensions that can make coalescing answer differently: how the
// second path reaches the shared node (a plain alias, or a merge key that carries the whole
// mapping into `database`), and which of the two strengths the schema walk records first.
//
// firstKind is what makes each name falsifiable: a fixture whose document order flipped would
// still coalesce to one replacement, so without it "partial path first" and "whole path first"
// would be two spellings of one case. Order follows the root mapping's own entry order, so
// `database` written before `listeners` records urlPassword first and after it records
// entireValue first. assertOneExtentTwoStrengths asserts the recorded order rather than
// trusting the document to produce it.
//
// The `λ` before the marker is deliberate: it is a two-byte rune, so a caret column derived by
// counting bytes lands one column right of one derived by counting runes and fails the geometry
// assertion.
var sharedExtentRendererCases = []struct {
	name      string
	firstKind sensitivity
	document  string
}{
	{
		name:      "alias partial path first",
		firstKind: urlPassword,
		document: `version: 1
database: {url: &shared ` + sharedExtentValue + `, λ: λ, tail: ` + sharedExtentTailMarker + `}
listeners:
- destination:
    signing:
      secrets: [*shared]
`,
	},
	{
		// The anchor is defined at a public scalar rather than inside the `secrets` container, and
		// that is forced rather than stylistic: an anchor name written inside a container the table
		// declares secret in full is author-written text inside that value, so the container is now
		// blanked through the end of its line and no tail survives there for a caret to be measured
		// against (containerinterior.go). YAML resolves an alias backwards, so the anchor cannot be
		// moved to the `database.url` site either without making this row's own order impossible.
		//
		// The tail is a sibling entry rather than a trailing comment for a second measured reason:
		// goccy folds a comment written after a scalar into that scalar's own String(), so a
		// replacement of the value's text takes the comment with it wherever the anchor sits.
		name:      "alias whole path first",
		firstKind: entireValue,
		document: `version: 1
defaults: {timeout: &shared ` + sharedExtentValue + `, λ: λ, tail: ` + sharedExtentTailMarker + `}
listeners:
- destination:
    signing:
      secrets: [*shared]
database:
  url: *shared
`,
	},
	{
		name:      "merge partial path first",
		firstKind: urlPassword,
		document: `version: 1
defaults:
  headers: &base {url: &shared ` + sharedExtentValue + `, λ: λ, tail: ` + sharedExtentTailMarker + `}
database:
  <<: *base
listeners:
- destination:
    signing:
      secrets: [*shared]
`,
	},
	{
		name:      "merge whole path first",
		firstKind: entireValue,
		document: `version: 1
defaults:
  headers: &base {url: &shared ` + sharedExtentValue + `, λ: λ, tail: ` + sharedExtentTailMarker + `}
listeners:
- destination:
    signing:
      secrets: [*shared]
database:
  <<: *base
`,
	},
}

// TestTheRendererCoalescesASharedExtentAtItsStrictestSensitivity fails when the coalescing call in
// redactDeclaredPaths is removed: the second replacement then rewrites runes the first already
// replaced, which both doubles the placeholder and destroys the tail the caret is derived against.
func TestTheRendererCoalescesASharedExtentAtItsStrictestSensitivity(t *testing.T) {
	for _, tc := range sharedExtentRendererCases {
		t.Run(tc.name, func(t *testing.T) {
			assertOneExtentTwoStrengths(t, tc.document, tc.firstKind)

			line, column := sharedExtentMarkerPosition(t, tc.document)
			rendered := Errors{{
				File: "listeners.yaml", Line: line, Col: column, Msg: sharedExtentMessage,
			}}.Render([]byte(tc.document))

			if count := strings.Count(rendered, redactionPlaceholder); count != 1 {
				t.Errorf("replacement count = %d, want one per shared extent:\n%s", count, rendered)
			}
			// SHAREDEXTENTHOST is the host, which the weaker urlPassword declaration keeps and the
			// stricter entireValue one removes, so its survival names *which* strength won rather
			// than merely that something was replaced.
			for _, removed := range []string{"SHAREDEXTENTPASSWORD", "SHAREDEXTENTHOST"} {
				if strings.Contains(rendered, removed) {
					t.Errorf("the strictest declaration did not win: %s survived:\n%s",
						removed, rendered)
				}
			}
			assertCaretAtMarker(t, rendered)
		})
	}
}

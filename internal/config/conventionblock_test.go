package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// The Diagnostic rendering convention's authoritative block, rendered by a fixture at last.
//
// Step 2 froze that block against a hand-built diagnostic and recorded why (its Note 9): a
// diagnostic existed then only for a document that failed to parse, and such a document leaves no
// root mapping, so no fixture in the corpus could reach the path-aware branch the block is drawn
// from. Its note asked this step to add a `colums` fixture when R41 landed. This is that closure.

// TestTheColumsFixtureRendersTheSpecificationsOwnBlock closes the loop Step 2's Note 9 left open:
// the convention's authoritative block was frozen there against a hand-built diagnostic, because
// no fixture could produce one until R41 existed. One does now, and it is laid out so that its
// rendered output is that block.
//
// The two differ in the filename alone, which the fixture-naming convention forces: a golden's
// header carries the fixture's own base name, and a fixture called `listeners.yaml` could not
// declare the rule it covers. Substituting the name rather than relaxing the comparison keeps the
// claim byte-exact everywhere else.
func TestTheColumsFixtureRendersTheSpecificationsOwnBlock(t *testing.T) {
	const name = "R41_did_you_mean_columns"

	path := fixture(name)
	data := readFixtureBytes(t, path)
	_, _, errs := Parse(data, filepath.Base(path), corpusEnvironment())

	want := strings.Replace(authoritativeBlock, "listeners.yaml:", filepath.Base(path)+":", 1)
	if got := errs.Render(data); got != want {
		t.Errorf("the fixture does not render the convention's block:\n%s", goldenDiff(want, got))
	}
}

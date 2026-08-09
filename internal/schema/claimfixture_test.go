package schema

import (
	"path/filepath"
	"strings"
	"testing"
)

// theClaimQueryFixture is the claim query as this package holds it. internal/source issues this text
// and pins it byte for byte against this file, so the fixture is the authority both packages read
// rather than a copy either of them keeps.
var theClaimQueryFixture = filepath.Join("testdata", "claim_query.sql")

// theCommentLeader opens every line of the fixture's header, which is where the fixture's body
// begins after it.
const theCommentLeader = "--"

// documentText is one file read as text. It composes the package's existing reader rather than
// repeating its body, so there is one place that reports an unreadable file.
func documentText(t *testing.T, path string) string {
	t.Helper()
	return string(sourceBytes(t, path))
}

// claimQueryText is the fixture's SQL: everything after the header comment recording where the query
// came from.
//
// The split is asserted rather than guessed. The header is the leading run of comment lines followed
// by exactly one blank line, and a fixture shaped otherwise fails here rather than silently handing
// its header to the planner.
func claimQueryText(t *testing.T) string {
	t.Helper()

	lines := strings.Split(documentText(t, theClaimQueryFixture), "\n")
	header := 0
	for header < len(lines) && strings.HasPrefix(lines[header], theCommentLeader) {
		header++
	}

	switch {
	case header == 0:
		t.Fatalf("%s opens with %q rather than with the header comment recording its source",
			theClaimQueryFixture, lines[0])
	case header+1 >= len(lines) || lines[header] != "":
		t.Fatalf("%s does not separate its header comment from its query with one blank line, so "+
			"where the query begins is a guess", theClaimQueryFixture)
	}
	return strings.Join(lines[header+1:], "\n")
}

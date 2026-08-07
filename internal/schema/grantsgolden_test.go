package schema

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The golden harness, following internal/config's convention rather than a new one: a byte
// comparison and an opt-in flag, with no third-party dependency.
//
// testdata/grants.golden was written by hand from the three documented statement families and the
// fixture below, never generated and then declared correct. It is the contract Step 16 applies
// against a real server, so it holds statements and nothing else -- no commentary a `psql -f` would
// have to strip.

// updateGrantsGolden is opt-in and off by default, so rewriting the contract is always a deliberate
// act. A harness that can rewrite its own expectation cannot fail.
var updateGrantsGolden = flag.Bool(grantsUpdateFlag, false,
	"rewrite testdata/grants.golden from the currently emitted statements")

const (
	grantsUpdateFlag  = "update"
	grantsUpdateHowTo = "go test ./internal/schema/ -run Golden -" + grantsUpdateFlag
)

var grantsGolden = filepath.Join("testdata", "grants.golden")

// renderedGrants is the emitted set as the golden holds it: one statement per line.
func renderedGrants() string {
	return strings.Join(GrantStatements(fixtureSchema, fixtureRole, fixtureTargets), "\n") + "\n"
}

// missingGrantsGoldenMessage is what a contributor whose first run finds no golden is told. It is a
// value rather than a t.Fatalf call site so it can be read without failing a test.
func missingGrantsGoldenMessage(rendered string) string {
	return grantsGolden + " does not exist. Check this output by hand against AC 41's three " +
		"statement families, then create it with `" + grantsUpdateHowTo + "`:\n" + rendered
}

// TestGrantStatementsMatchTheGolden is the contract with the DBA, compared byte for byte: a
// normalised or line-wise comparison would let whitespace and ordering drift.
func TestGrantStatementsMatchTheGolden(t *testing.T) {
	rendered := renderedGrants()
	if *updateGrantsGolden {
		writeGrantsGolden(t, rendered)
		return
	}
	compareGrantsGolden(t, rendered)
}

// compareGrantsGolden is the comparison itself, shared with the no-write guard so that guard
// exercises the assertion rather than a second expression of it.
func compareGrantsGolden(t *testing.T, rendered string) {
	t.Helper()

	recorded, err := os.ReadFile(grantsGolden)
	if err != nil {
		t.Fatal(missingGrantsGoldenMessage(rendered))
	}
	if string(recorded) != rendered {
		t.Errorf("the emitted statements and the golden have parted:\n--- golden ---\n%s"+
			"--- emitted ---\n%s", recorded, rendered)
	}
}

// TestAMissingGoldenNamesTheCommandThatCreatesIt reads the message rather than provoking it, so the
// instruction a contributor is given is asserted to name a flag this harness registers -- a message
// naming a flag it does not register teaches the wrong command.
func TestAMissingGoldenNamesTheCommandThatCreatesIt(t *testing.T) {
	message := missingGrantsGoldenMessage(renderedGrants())

	if !strings.Contains(message, "-"+grantsUpdateFlag) {
		t.Errorf("the missing-golden message does not name the flag:\n%s", message)
	}
	if flag.Lookup(grantsUpdateFlag) == nil {
		t.Errorf("this binary registers no %q flag, so the command the message teaches would fail",
			grantsUpdateFlag)
	}
	if !strings.Contains(message, grantsGolden) {
		t.Errorf("the missing-golden message does not name the file:\n%s", message)
	}
}

// TestARunWithoutUpdateNeverWritesTheGolden proves the gate rather than assuming it: the comparison
// runs between two readings of the file's modification time, so a write on the default path shows
// up here as a changed timestamp.
func TestARunWithoutUpdateNeverWritesTheGolden(t *testing.T) {
	if *updateGrantsGolden {
		t.Skip("-update is set, so writing the golden is the point of this run")
	}
	before := grantsGoldenWrittenAt(t)

	compareGrantsGolden(t, renderedGrants())

	if after := grantsGoldenWrittenAt(t); !after.Equal(before) {
		t.Errorf("%s was written during a run without -%s: %v then %v",
			grantsGolden, grantsUpdateFlag, before, after)
	}
}

// grantsGoldenWrittenAt is the golden's modification time, and fails when it is absent so the guard
// above cannot pass by comparing two readings of nothing.
func grantsGoldenWrittenAt(t *testing.T) time.Time {
	t.Helper()

	info, err := os.Stat(grantsGolden)
	if err != nil {
		t.Fatalf("stat %s: %v", grantsGolden, err)
	}
	return info.ModTime()
}

// writeGrantsGolden records the contract under the flag.
func writeGrantsGolden(t *testing.T, rendered string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(grantsGolden), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(grantsGolden), err)
	}
	if err := os.WriteFile(grantsGolden, []byte(rendered), 0o644); err != nil {
		t.Fatalf("write %s: %v", grantsGolden, err)
	}
}

// TestTheGoldenHoldsOnlyStatements keeps the file applicable as it stands. Step 16 reads it and
// applies every line to a real server, so a comment or a blank line here becomes a statement there.
func TestTheGoldenHoldsOnlyStatements(t *testing.T) {
	recorded, err := os.ReadFile(grantsGolden)
	if err != nil {
		t.Fatal(missingGrantsGoldenMessage(renderedGrants()))
	}

	lines := strings.Split(strings.TrimSuffix(string(recorded), "\n"), "\n")
	if len(lines) != 3+len(fixtureTargets) {
		t.Fatalf("the golden holds %d lines, want %d", len(lines), 3+len(fixtureTargets))
	}
	for _, line := range lines {
		if familyOf(line) == familyUnknown {
			t.Errorf("the golden holds a line outside the documented set: %s", line)
		}
		if !strings.HasSuffix(line, ";") {
			t.Errorf("the golden holds a line that is not a terminated statement: %s", line)
		}
	}
}

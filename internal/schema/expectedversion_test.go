package schema

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestExpectedVersionIsTheHighestMigrationTheDirectoryHolds takes its authority from the migration
// directory on disk rather than from embeddedCorpus, which is the reader ExpectedVersion answers
// through. Derived from the corpus, the assertion would be satisfied by construction: a reader that
// stopped seeing the last file would agree with itself and report a database as current while a
// migration it had never applied was still pending.
func TestExpectedVersionIsTheHighestMigrationTheDirectoryHolds(t *testing.T) {
	entries, err := os.ReadDir(migrationDir)
	if err != nil {
		t.Fatalf("reading %s failed: %v", migrationDir, err)
	}

	highest, counted := 0, 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		underscore := strings.IndexByte(name, '_')
		if underscore < 0 {
			t.Fatalf("migration %q declares no version before an underscore", name)
		}
		version, err := strconv.Atoi(name[:underscore])
		if err != nil {
			t.Fatalf("migration %q does not begin with a version: %v", name, err)
		}
		counted++
		if version > highest {
			highest = version
		}
	}
	if counted == 0 {
		t.Fatalf("%s holds no .sql file, so this test would pass over nothing", migrationDir)
	}

	got, err := ExpectedVersion()
	if err != nil {
		t.Fatalf("ExpectedVersion failed: %v", err)
	}
	if got != highest {
		t.Errorf("ExpectedVersion = %d, want %d, the highest version the %d files in %s declare",
			got, highest, counted, migrationDir)
	}
}

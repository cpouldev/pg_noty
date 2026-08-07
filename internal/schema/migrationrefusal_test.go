package schema

import (
	"errors"
	"strings"
	"testing"
)

// The four reasons corpusFrom refuses a corpus, and the order that selects between them. They live
// apart from migrations_test.go because they are one subject: what an operator is told when the
// corpus is wrong, and which of two simultaneous faults they are told about.

// malformations are the corpus shapes that must be refused, each with the reason it is refused
// under. A duplicate necessarily breaks consecutiveness too, so no input can isolate the duplicate
// clause from the gap clause; the row therefore asserts the reason rather than merely the refusal,
// and the order that selects between them is pinned separately below.
var malformations = []struct {
	name  string
	files map[string]string
	want  error
	// names is text the message must carry, so a refusal that cannot tell an operator which file
	// to fix fails here.
	names string
}{
	{name: "a version gap", want: errCorpusGap, names: "0003_third.sql",
		files: map[string]string{"0001_first.sql": "a\n", "0003_third.sql": "c\n"}},
	{name: "a duplicate version", want: errCorpusDuplicate, names: "0001_second.sql",
		files: map[string]string{"0001_first.sql": "a\n", "0001_second.sql": "b\n"}},
	{name: "an unparseable filename", want: errCorpusFilename, names: "ledger.sql",
		files: map[string]string{"0001_first.sql": "a\n", "ledger.sql": "b\n"}},
	{name: "a version below the first", want: errCorpusVersionRange, names: "0000_zeroth.sql",
		files: map[string]string{"0000_zeroth.sql": "a\n", "0001_first.sql": "b\n"}},
}

func TestEachCorpusMalformationIsRefusedUnderItsOwnReason(t *testing.T) {
	for _, tc := range malformations {
		t.Run(tc.name, func(t *testing.T) {
			_, err := corpusOf(tc.files)
			if err == nil {
				t.Fatalf("a corpus holding %s was accepted", tc.name)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("refused with %v, want the reason %v", err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.names) {
				t.Errorf("the refusal reads %q and does not name %q, so an operator is not told "+
					"which file to fix", err, tc.names)
			}
		})
	}
}

// TestTheCorpusRefusalsAreMutuallyDistinguishable crosses every malformation against every reason.
// Four reasons collapsed into one sentinel would pass the test above -- every row would match --
// and fail here on the twelve cells that must not match.
func TestTheCorpusRefusalsAreMutuallyDistinguishable(t *testing.T) {
	if len(malformations) != 4 {
		t.Fatalf("%d malformations are enumerated; update this count with the set", len(malformations))
	}

	for _, tc := range malformations {
		_, err := corpusOf(tc.files)
		for _, other := range malformations {
			matched := errors.Is(err, other.want)
			if want := other.name == tc.name; matched != want {
				t.Errorf("%s refused with %v, and errors.Is against %s answers %t, want %t",
					tc.name, err, other.name, matched, want)
			}
		}
	}
}

// TestTheDocumentedRefusalPrecedenceIsWhatSelects gives every documented ordering a corpus that
// breaks both of its rules at once. Without such a corpus each input breaks one rule only, both
// orders answer identically, and the order could be reversed with the suite green.
func TestTheDocumentedRefusalPrecedenceIsWhatSelects(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  error
	}{
		{name: "an unparseable filename beside a gap", want: errCorpusFilename,
			files: map[string]string{"0001_first.sql": "a\n", "ledger.sql": "b\n",
				"0004_fourth.sql": "c\n"}},
		{name: "a duplicate version, which is also a gap", want: errCorpusDuplicate,
			files: map[string]string{"0001_first.sql": "a\n", "0001_second.sql": "b\n"}},
		{name: "a version below the first, which is also a gap", want: errCorpusVersionRange,
			files: map[string]string{"0000_zeroth.sql": "a\n", "0002_second.sql": "b\n"}},
		{name: "an unparseable filename beside a duplicate", want: errCorpusFilename,
			files: map[string]string{"0001_first.sql": "a\n", "0001_second.sql": "b\n",
				"ledger.sql": "c\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := corpusOf(tc.files)
			if !errors.Is(err, tc.want) {
				t.Errorf("refused with %v, want the documented %v", err, tc.want)
			}
		})
	}
}

// TestAnEntryThatIsNoMigrationFileIsRefusedRatherThanSkipped is the fail-closed side of the reader:
// a directory, or a README, inside the corpus directory is refused by name rather than passed over,
// because an entry silently skipped is a migration silently not applied. The four rows are the four
// ways the filename rule can be missed: a wrong extension, the right extension with the wrong stem,
// an empty version, and no separator at all.
func TestAnEntryThatIsNoMigrationFileIsRefusedRatherThanSkipped(t *testing.T) {
	for _, name := range []string{"README.md", "0002_objects.txt", "_leading.sql", "0002.sql"} {
		t.Run(name, func(t *testing.T) {
			_, err := corpusOf(map[string]string{"0001_first.sql": "a\n", name: "x\n"})
			if !errors.Is(err, errCorpusFilename) {
				t.Errorf("an entry named %q was answered with %v, want a filename refusal", name, err)
			}
		})
	}
}

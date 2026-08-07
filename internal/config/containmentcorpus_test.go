package config

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// This file makes a committed corpus entry's finding independent of the layout table's length.
//
// FuzzRenderedTextNeverQuotesASecret's signature is byte-oriented, so an entry stores its layout and
// its key spelling as ordinals resolved by `table[n % len(table)]`. An ordinal is a *position*, and
// the position's meaning belongs to a list every round extends: secretLayouts grew from 22 rows to
// 53, and six of the eight numeric entries silently began selecting a layout that did not exist when
// they were minted. Re-keying each entry to the layout it was minted against is not available: the
// nine entries were written across three days at three different table sizes, so no single modulus
// reconstructs the pairing and any name written here would be invented rather than measured.
//
// What every entry does carry unchanged is its *tail* -- the generated secret bytes, which is the
// half the fuzzer actually searched and the half no table can renumber. So the tails are declared
// here and the seed grid crosses them with *every* layout and *every* key spelling. Whichever
// pairing an entry was minted for is therefore reached on every plain `go test`, and no growth of
// secretLayouts can move it, because the restoration indexes by nothing.

// corpusMintedTails are the secrets `go test -fuzz` wrote into
// testdata/fuzz/FuzzRenderedTextNeverQuotesASecret/, each named for the entry carrying it so that a
// failure in the seed grid names the finding rather than a hash. Both directions are reconciled
// against the directory by TestEveryCommittedCorpusEntryTailIsCrossedWithTheWholeGrid.
var corpusMintedTails = map[string]string{
	"31058a1edb779c34": "]0\n0000000000000000000000000000000000000000000000000000000000000\n00: ",
	"46cb793b1852f94e": "\n: ",
	"62b1b0ceeb71174c": "]\n: ",
	"6d59c70dfe3b1534": " A=",
	"7556dfa036a9dacc": "\r:",
	// Minted by the round-eight session, against the node-shape family added the same round: written
	// raw into a block-sequence item, this tail's `<<` makes the parser read a merge key, stage E
	// refuses the merge and removes the entry, and the mapping left behind has no key of its own to
	// redact from. It is the third route to one leak, and the reason writtenStartOf answers a block
	// mapping from its line rather than from its first key.
	"75a88fd533a33bac": "<<: ",
	"85c4d763c0bdf844": "}\n0\r0",
	"876b6732f796486a": "]#\n#0",
	// Minted by the round-nine session, on the first fuzz run after the generator stopped excluding
	// byte classes from the payload. Written raw into a flow sequence spanning two lines, this tail's
	// `&` makes the parser read an anchor property, stage E drops that property from the tree, and
	// its name -- author-written text inside `secrets:` -- belonged to no value afterwards. It is why
	// redactValue now reads the parser's own extent as a reason to blank wholesale, rather than only
	// as a reach to walk once some other test had already fired.
	"8f561b44265cd89e": ",&\"",
	"a_block_scalar_secret_split_by_a_carriage_return": "\r: ",
	"d9c08125011c9b7c": "]\r0\n0000000000000000000000: ",
}

const fuzzCorpusDirectory = "testdata/fuzz/FuzzRenderedTextNeverQuotesASecret"

// fuzzCorpusEntry is one committed corpus file as the target reads it.
type fuzzCorpusEntry struct {
	name     string
	tail     string
	shape    uint8
	spelling uint8
}

// TestEveryCommittedCorpusEntryTailIsCrossedWithTheWholeGrid reconciles the declared tails against
// the directory in both directions, so a fuzzing session that commits a new entry fails here until
// its tail joins the grid -- rather than leaving an entry whose only reach is an ordinal that drifts.
func TestEveryCommittedCorpusEntryTailIsCrossedWithTheWholeGrid(t *testing.T) {
	entries := readFuzzCorpus(t)
	if len(entries) != len(corpusMintedTails) {
		t.Fatalf("%d committed corpus entries and %d declared tails; declare the tail of every entry "+
			"so the grid reaches its finding whatever its ordinals now select",
			len(entries), len(corpusMintedTails))
	}

	crossed := seedTails()
	for _, entry := range entries {
		declared, wired := corpusMintedTails[entry.name]
		if !wired {
			t.Errorf("corpus entry %s carries no declared tail, so nothing crosses its secret with the "+
				"layout grid and its finding rests on an ordinal that renumbers", entry.name)
			continue
		}
		if declared != entry.tail {
			t.Errorf("corpus entry %s carries tail %q, declared as %q", entry.name, entry.tail, declared)
		}
		// Declaring the tail is not what restores the finding; crossing it with the grid is. Asserted
		// against seedTails itself so that unwiring the cross fails here rather than leaving a
		// manifest that reconciles perfectly and reaches nothing.
		if !slices.Contains(crossed, entry.tail) {
			t.Errorf("corpus entry %s's tail is declared but not among the grid's seed tails, so no "+
				"layout but the one its ordinal selects ever receives it", entry.name)
		}
	}
}

// TestEveryCommittedCorpusEntryLeaksNothingThroughItsOwnSelection runs each committed entry exactly
// as the target does -- its own tail, its own two ordinals -- so the claim that the corpus is
// answered is checkable here rather than asserted in a comment.
func TestEveryCommittedCorpusEntryLeaksNothingThroughItsOwnSelection(t *testing.T) {
	for _, entry := range readFuzzCorpus(t) {
		layout := secretLayouts[int(entry.shape)%len(secretLayouts)]
		key := keySpellings[int(entry.spelling)%len(keySpellings)]
		t.Run(entry.name+", "+layout.name, func(t *testing.T) {
			assertPlantedSecretsAreContained(t, entry.tail, layout, key)
		})
	}
}

func readFuzzCorpus(t *testing.T) []fuzzCorpusEntry {
	t.Helper()

	listed, err := os.ReadDir(fuzzCorpusDirectory)
	if err != nil {
		t.Fatalf("read %s: %v", fuzzCorpusDirectory, err)
	}
	entries := make([]fuzzCorpusEntry, 0, len(listed))
	for _, file := range listed {
		if file.IsDir() {
			continue
		}
		entries = append(entries, parseFuzzCorpusEntry(t, file.Name()))
	}
	if len(entries) == 0 {
		t.Fatalf("%s holds no corpus entry, so every assertion over it would pass vacuously",
			fuzzCorpusDirectory)
	}
	return entries
}

// parseFuzzCorpusEntry reads the `go test fuzz v1` format the toolchain writes: a version line, then
// one line per argument of the target's signature.
func parseFuzzCorpusEntry(t *testing.T, name string) fuzzCorpusEntry {
	t.Helper()

	path := filepath.Join(fuzzCorpusDirectory, name)
	lines := strings.Split(strings.TrimRight(string(readFixtureBytes(t, path)), "\n"), "\n")
	if len(lines) != 4 || lines[0] != "go test fuzz v1" {
		t.Fatalf("%s holds %d lines headed %q, want the four of a (string, uint8, uint8) entry",
			name, len(lines), lines[0])
	}

	return fuzzCorpusEntry{
		name:     name,
		tail:     corpusArgument(t, name, lines[1], "string"),
		shape:    corpusByteArgument(t, name, lines[2]),
		spelling: corpusByteArgument(t, name, lines[3]),
	}
}

// corpusByteArgument decodes one `byte(...)` line. The toolchain writes it as a *rune* literal, so
// the value is the literal's code point and not its encoding: 129 is written `byte('')`, whose
// UTF-8 form is two bytes. Reading the encoding answered "two bytes, want one" and made every
// committed entry holding an ordinal above 0x7F unreadable -- which is not a smaller corpus but a
// corpus that cannot be read at all, since readFuzzCorpus fails the whole file.
func corpusByteArgument(t *testing.T, name, line string) uint8 {
	t.Helper()

	written := []rune(corpusArgument(t, name, line, "byte"))
	if len(written) != 1 || written[0] > maxByteValue {
		t.Fatalf("%s: byte argument %q decodes to %q, want one code point below %d",
			name, line, string(written), maxByteValue+1)
	}
	return uint8(written[0])
}

// maxByteValue is the largest ordinal a uint8 argument of the target can carry.
const maxByteValue = 255

// corpusArgument decodes one `kind(literal)` line, refusing any other kind so that a signature
// change fails here by name instead of decoding one argument as another.
func corpusArgument(t *testing.T, name, line, kind string) string {
	t.Helper()

	literal, enclosed := strings.CutPrefix(line, kind+"(")
	if !enclosed || !strings.HasSuffix(literal, ")") {
		t.Fatalf("%s: %q is no %s argument; the target's signature and this reader disagree",
			name, line, kind)
	}
	written, err := strconv.Unquote(strings.TrimSuffix(literal, ")"))
	if err != nil {
		t.Fatalf("%s: cannot decode %s argument %q: %v", name, kind, line, err)
	}
	return written
}

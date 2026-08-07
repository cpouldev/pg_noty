package schema

import (
	"regexp"
	"strconv"
	"testing"
	"testing/fstest"
)

// syntheticDir is where corpusOf plants a synthetic corpus. It is not "migrations", so a test
// reading a synthetic corpus cannot accidentally read the embedded one.
const syntheticDir = "corpus"

// corpusOf parses a synthetic corpus through the same reader the embedded one goes through, so a
// refusal asserted here is the refusal the shipped corpus would meet.
func corpusOf(files map[string]string) ([]migration, error) {
	planted := fstest.MapFS{}
	for name, text := range files {
		planted[syntheticDir+"/"+name] = &fstest.MapFile{Data: []byte(text)}
	}
	return corpusFrom(planted, syntheticDir)
}

func TestTheEmbeddedCorpusIsTwoMigrationsNumberedOneAndTwo(t *testing.T) {
	corpus := embeddedCorpusOrFail(t)

	if len(corpus) != 2 {
		t.Fatalf("the embedded corpus holds %d migrations, want the two ADR-10 splits it into: "+
			"the ledger alone, then every other object", len(corpus))
	}
	for i, want := range []struct {
		version int
		file    string
	}{
		{version: 1, file: "0001_ledger.sql"},
		{version: 2, file: "0002_objects.sql"},
	} {
		if corpus[i].version != want.version || corpus[i].file != want.file {
			t.Errorf("migration %d is version %d from %q, want version %d from %q",
				i, corpus[i].version, corpus[i].file, want.version, want.file)
		}
		if corpus[i].sql == "" {
			t.Errorf("migration %d carries no text", corpus[i].version)
		}
	}
}

// shippedFilename is the Migration contract's naming convention, migrations/NNNN_<slug>.sql, pinned
// against the contract's own literal rather than against what the reader happens to accept.
var shippedFilename = regexp.MustCompile(`^[0-9]{4}_[a-z][a-z0-9_]*\.sql$`)

// TestTheShippedFilenamesFollowTheFourDigitConvention pins the convention separately from the
// reader, which deliberately accepts an unpadded version as well. The looseness is not an oversight:
// zero-padding exists to make lexical order agree with numeric order, so a corpus written only with
// padded names cannot tell an implementation that sorts by version from one that returns the
// directory's own order, and the ordering assertion below would have nothing to fail on. Step 10's
// synthetic corpora inherit the same latitude.
func TestTheShippedFilenamesFollowTheFourDigitConvention(t *testing.T) {
	for _, found := range embeddedCorpusOrFail(t) {
		if !shippedFilename.MatchString(found.file) {
			t.Errorf("%q is not the contract's migrations/NNNN_<slug>.sql", found.file)
		}
	}
}

// TestACorpusIsOrderedByItsFilenameDerivedVersion reads the corpus in an order that disagrees with
// the answer. fs.ReadDir sorts by filename, and these filenames are unpadded, so the directory
// hands "10" back before "1" and every version between them: an implementation that returned the
// read order, or that sorted the names as text, answers 10,1,2..9 and fails here.
func TestACorpusIsOrderedByItsFilenameDerivedVersion(t *testing.T) {
	files := map[string]string{}
	for version := firstMigrationVersion; version <= 10; version++ {
		numbered := strconv.Itoa(version)
		files[numbered+"_step.sql"] = "-- version " + numbered + "\n"
	}

	corpus, err := corpusOf(files)
	if err != nil {
		t.Fatalf("a corpus of ten consecutive versions was refused: %v", err)
	}
	if len(corpus) != 10 {
		t.Fatalf("read %d migrations from ten files", len(corpus))
	}
	for i, found := range corpus {
		if want := firstMigrationVersion + i; found.version != want {
			t.Errorf("position %d holds version %d from %q, want %d",
				i, found.version, found.file, want)
		}
	}
}

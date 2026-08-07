package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strings"
	"testing"
)

// schemaPlaceholder is the interpolation D1 rejected, written here and nowhere in the SQL. The
// checksum's schema independence is only a measurement if something could have made it depend on a
// schema, so the rejected design is reproduced in the test as the substitute it would have been.
const schemaPlaceholder = "${SCHEMA}"

// underSchema is what a ${SCHEMA}-placeholder corpus would have executed for a given schema. On
// the shipped files it is the identity, which is the property D1 buys and which
// TestTheChecksumWouldMoveWithTheSchemaIfAPlaceholderWerePresent proves is not free.
func underSchema(sql, schema string) string {
	return strings.ReplaceAll(sql, schemaPlaceholder, schema)
}

func checksumOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// TestEachChecksumIsSha256OverTheExactEmbeddedBytes reads the bytes back from the embedded
// filesystem rather than from the parsed corpus, so a reader that normalised line endings, trimmed
// the file or lower-cased it before hashing fails here. The two trailing clauses name the
// transforms that would otherwise be invisible: both of these files end in a newline, so a hash
// taken over the trimmed text is a different hash, and asserting the difference is what makes
// "exact bytes" falsifiable rather than a restatement of the implementation.
func TestEachChecksumIsSha256OverTheExactEmbeddedBytes(t *testing.T) {
	for _, found := range embeddedCorpusOrFail(t) {
		data, err := embeddedMigrations.ReadFile(path.Join(migrationDir, found.file))
		if err != nil {
			t.Fatalf("read %s back from the embedded filesystem: %v", found.file, err)
		}

		if found.sql != string(data) {
			t.Errorf("%s carries text that is not its own bytes, so the checksum below covers "+
				"something other than what would be executed", found.file)
		}
		if want := checksumOf(string(data)); found.checksum != want {
			t.Errorf("%s has checksum %q, want sha256 over its exact bytes %q",
				found.file, found.checksum, want)
		}
		if trimmed := checksumOf(strings.TrimSpace(string(data))); found.checksum == trimmed {
			t.Errorf("%s hashes the same trimmed as untrimmed, so the checksum is taken over a "+
				"normalised form rather than over the exact bytes", found.file)
		}
	}
}

// TestOneChangedByteChangesAChecksum pins both directions. Sensitivity alone is satisfied by a
// checksum that is different every call -- a timestamp, a random value -- so the identical-bytes
// row is what makes the sensitive row mean anything.
func TestOneChangedByteChangesAChecksum(t *testing.T) {
	const original = "CREATE TABLE ledger (version int NOT NULL);\n"
	// One byte differs: the final `t` of `int` becomes `T`. Nothing else about the file moves.
	const oneByteApart = "CREATE TABLE ledger (version inT NOT NULL);\n"

	first := singleMigrationChecksum(t, original)
	if again := singleMigrationChecksum(t, original); first != again {
		t.Fatalf("the same bytes checksummed to %q and then %q", first, again)
	}
	if changed := singleMigrationChecksum(t, oneByteApart); changed == first {
		t.Errorf("a file differing in one byte checksummed to %q as well, so an edit after the "+
			"migration was applied would be undetectable", changed)
	}
}

func singleMigrationChecksum(t *testing.T, text string) string {
	t.Helper()

	corpus, err := corpusOf(map[string]string{"0001_only.sql": text})
	if err != nil {
		t.Fatalf("a one-file corpus was refused: %v", err)
	}
	return corpus[0].checksum
}

// TestAChecksumDoesNotDependOnTheSchemaInForce is D1's half of the claim: because both files carry
// unqualified names and the only interpolation in the migration path is the runner's SET LOCAL
// search_path, the text executed under schema noty and under schema tenant_a is the same text, so
// the checksum covers exactly what was executed.
func TestAChecksumDoesNotDependOnTheSchemaInForce(t *testing.T) {
	for _, found := range embeddedCorpusOrFail(t) {
		underDefault := checksumOf(underSchema(found.sql, "noty"))
		underTenant := checksumOf(underSchema(found.sql, "tenant_a"))

		if underDefault != underTenant {
			t.Errorf("%s checksums differently under two schemas, so its text carries an "+
				"interpolation and the checksum covers a form rather than the executed text",
				found.file)
		}
		if found.checksum != underDefault {
			t.Errorf("%s has checksum %q, and the text a schema would execute checksums to %q",
				found.file, found.checksum, underDefault)
		}
	}
}

// TestTheChecksumWouldMoveWithTheSchemaIfAPlaceholderWerePresent is the assertion above made
// falsifiable. underSchema is the identity on the shipped files, so without this control the test
// above would pass against a corpus of empty files, against a corpus of placeholders it forgot to
// substitute, and against a substitution that did nothing.
func TestTheChecksumWouldMoveWithTheSchemaIfAPlaceholderWerePresent(t *testing.T) {
	planted := "CREATE TABLE " + schemaPlaceholder + ".ledger (version int NOT NULL);\n"

	if checksumOf(underSchema(planted, "noty")) == checksumOf(underSchema(planted, "tenant_a")) {
		t.Fatal("a text holding the placeholder checksums the same under two schemas, so the " +
			"schema-independence assertion above cannot fail and is not evidence")
	}
}

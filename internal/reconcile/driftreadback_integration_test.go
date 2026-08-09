//go:build integration

package reconcile

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Two questions the whole drift suite asks about a readback, answered once here rather than in each
// file that asks them: how one is taken from a second process, and how one is compared with the text
// the generator wrote. Both are shared -- the round-trip premise asks for a corpus member read back
// "in a second process" and the pinned readings must be identical "across.. processes"; the ten
// divergences all compare a readback with a generated text -- so a copy per file
// would be several behaviours the moment one of them gained a guard. This file was split out of
// driftdivergence_integration_test.go when repairing that file's two catalog-only cases took it past
// the 200-line budget.

// driftReadbackEnvironment carries one reading's coordinates to a child process.
const driftReadbackEnvironment = "PGNOTY_DRIFT_READBACK"

// TestDriftReadbackSubprocess is the child process's entry point rather than an assertion of its own:
// it reads one pair on a pinned connection and writes it back to its parent, which compares.
func TestDriftReadbackSubprocess(t *testing.T) {
	skipIfShort(t)
	if os.Getenv(driftReadbackEnvironment) == "" {
		return
	}
	pool, err := pgxpool.New(context.Background(), os.Getenv(driftReadbackEnvironment+"_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	oid64, err := strconv.ParseUint(os.Getenv(driftReadbackEnvironment+"_OID"), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	pair := readCatalogPair(t, pool, uint32(oid64), os.Getenv(driftReadbackEnvironment+"_TRIGGER"), 0, false)
	_, _ = os.Stderr.WriteString(base64.StdEncoding.EncodeToString([]byte(pair[0])) + ":" + base64.StdEncoding.EncodeToString([]byte(pair[1])))
}

func driftReadbackInSubprocess(t *testing.T, pool *pgxpool.Pool, target, trigger string) [2]string {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestDriftReadbackSubprocess$")
	command.Env = append(
		os.Environ(),
		driftReadbackEnvironment+"=1",
		driftReadbackEnvironment+"_DSN="+harnessConfig(t).Database.URL,
		driftReadbackEnvironment+"_OID="+strconv.FormatUint(uint64(resolvedCatalogTarget(t, pool, target).OID), 10),
		driftReadbackEnvironment+"_TRIGGER="+trigger,
	)
	var output bytes.Buffer
	command.Stderr = &output
	if err := command.Run(); err != nil {
		t.Fatalf("subprocess readback: %v: %s", err, output.String())
	}
	parts := strings.Split(strings.TrimSpace(output.String()), ":")
	if len(parts) != 2 {
		t.Fatalf("subprocess readback output %q", output.String())
	}
	return [2]string{decodeDriftReadback(t, parts[0]), decodeDriftReadback(t, parts[1])}
}

func decodeDriftReadback(t *testing.T, value string) string {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(decoded)
}

// diverges reports that the server returned returnedForm where the generator wrote wroteForm. The
// third clause is what makes this a comparison rather than a description of the reading: a server
// that started returning the generator's own form satisfies the first two and fails here.
func diverges(written, returned, wroteForm, returnedForm string) bool {
	return strings.Contains(written, wroteForm) && strings.Contains(returned, returnedForm) &&
		!strings.Contains(returned, wroteForm)
}

// aCastWasInserted is bare-literal-cast's own predicate rather than a diverges call, because the form
// the server returned carries the form the generator wrote inside it -- '1' within '1'::text -- so
// diverges' "the reading does not carry the written form" clause would be false for a real divergence.
func aCastWasInserted(written, returned, literal string) bool {
	return strings.Contains(written, literal) && !strings.Contains(written, literal+"::text") &&
		strings.Contains(returned, literal+"::text")
}

// noReadingCarriesATrailingSemicolon covers both readings, because the claim -- "the
// generator's trailing `;` which no reading carries" -- is about every reading, not the function's alone.
func noReadingCarriesATrailingSemicolon(set source.ObjectSet, pair [2]string) bool {
	return strings.HasSuffix(set.CreateTrigger, ";") && !strings.HasSuffix(pair[0], ";") &&
		strings.HasSuffix(set.CreateFunction, ";") && !strings.HasSuffix(pair[1], ";")
}

// generatedDollarTag is the tag the generator chose, read out of its own text. A hard-coded tag would
// assert nothing once source.dollarQuoteTag ascends past its base on a body that already contains it.
func generatedDollarTag(createFunction string) string {
	const opener = "\nAS $"
	opened := strings.Index(createFunction, opener)
	if opened < 0 {
		return ""
	}
	tag, _, found := strings.Cut(createFunction[opened+len(opener):], "$")
	if !found {
		return ""
	}
	return tag
}

// mustDDLQualified and mustDDLQuoted spell a name through the quoting authority the generator itself
// uses, so a change in that authority moves the written side of every comparison with it rather than
// leaving a stale literal behind.
func mustDDLQualified(t *testing.T, schemaName, name string) string {
	t.Helper()
	qualified, err := ddlQualified("divergence", schemaName, name)
	if err != nil {
		t.Fatal(err)
	}
	return qualified
}

func mustDDLQuoted(t *testing.T, name string) string {
	t.Helper()
	quoted, err := ddlQuoted("divergence", name)
	if err != nil {
		t.Fatal(err)
	}
	return quoted
}

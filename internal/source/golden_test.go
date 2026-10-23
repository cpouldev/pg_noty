package source

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

var updateGoldens = flag.Bool("update", false, "rewrite internal/source golden files")

type goldenMode struct {
	name    string
	payload config.Payload
}

type goldenCase struct {
	name    string
	request Request
}

var generationOperations = []string{"insert", "update", "delete"}
var generationModes = []goldenMode{
	{name: "full", payload: config.Payload{Mode: "full"}},
	{name: "full_exclude", payload: config.Payload{Mode: "full", Exclude: []string{"secret", "token"}}},
	{name: "columns", payload: config.Payload{Mode: "columns", Columns: []string{"id", "status"}}},
	{name: "keys_only", payload: config.Payload{Mode: "keys_only"}},
}
var generationIncludeOld = []bool{false, true}

// gridGoldenName spells one cell's golden. The crossed rows and the include_old pair assertions in
// goldenpairs_test.go both derive their file names through it, so no assertion can read a
// hand-listed set of names that has drifted from the corpus.
func gridGoldenName(operation, mode string, includeOld bool) string {
	return fmt.Sprintf("grid_%s_%s_old_%t.golden", operation, mode, includeOld)
}

func gridCases() []goldenCase {
	var cases []goldenCase
	for _, operation := range generationOperations {
		for _, mode := range generationModes {
			for _, includeOld := range generationIncludeOld {
				request := generationRequest(config.Operation{Kind: operation})
				request.Listener.Trigger.Payload = mode.payload
				request.Listener.Trigger.Payload.IncludeOld = includeOld
				cases = append(
					cases, goldenCase{
						name: gridGoldenName(operation, mode.name, includeOld), request: request,
					},
				)
			}
		}
	}
	return cases
}

func namedGoldenCases() []goldenCase {
	when := generationRequest(config.Operation{Kind: "update", When: "NEW.status IS DISTINCT FROM OLD.status"})
	columns := generationRequest(config.Operation{Kind: "update", Columns: []string{"status", "total"}})
	hostile := generationRequest(config.Operation{Kind: "insert"})
	hostile.Target.Table = `orders"; DROP TABLE users; --`
	distinctColumns := generationRequest(config.Operation{
		Kind: "update", Columns: []string{"Status", `a"b`}, IsDistinct: true,
	})
	distinctRow := generationRequest(config.Operation{Kind: "update", IsDistinct: true})
	distinctAndWhen := generationRequest(config.Operation{
		Kind: "update", Columns: []string{"status"}, IsDistinct: true,
		When: "NEW.enabled OR NEW.priority > 10",
	})
	return []goldenCase{
		{name: "named_when_update.golden", request: when},
		{name: "named_columns_update.golden", request: columns},
		{name: "named_hostile_catalog_table.golden", request: hostile},
		{name: "named_distinct_columns_update.golden", request: distinctColumns},
		{name: "named_distinct_row_update.golden", request: distinctRow},
		{name: "named_distinct_and_when_update.golden", request: distinctAndWhen},
	}
}

func allGoldenCases() []goldenCase { return append(gridCases(), namedGoldenCases()...) }

func goldenPath(name string) string { return filepath.Join("testdata", name) }

// committedGolden is the one reader of a member of the corpus: the comparator below and the
// include_old pair assertions both go through it.
func committedGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(goldenPath(name))
	if err != nil {
		t.Fatalf(
			"read %s: %v; run with -update only when changing the corpus intentionally",
			goldenPath(name), err,
		)
	}
	return string(data)
}

func goldenText(set ObjectSet) string {
	return strings.Join(
		[]string{
			"CreateFunction\n" + set.CreateFunction,
			"RevokeExecute\n" + set.RevokeExecute,
			"CommentFunction\n" + set.CommentFunction,
			"CreateTrigger\n" + set.CreateTrigger,
			"CommentTrigger\n" + set.CommentTrigger,
		}, "\n\n",
	) + "\n"
}

func generatedGolden(t *testing.T, request Request) string {
	t.Helper()
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 1 {
		t.Fatalf("golden request produced %d object sets", len(sets))
	}
	return goldenText(sets[0])
}

func checkOrUpdateGolden(t *testing.T, testCase goldenCase) {
	t.Helper()
	want := generatedGolden(t, testCase.request)
	if *updateGoldens {
		if err := os.WriteFile(goldenPath(testCase.name), []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if committedGolden(t, testCase.name) != want {
		t.Errorf("%s differs from generated text", goldenPath(testCase.name))
	}
}

func TestGeneratedSQLMatchesGoldens(t *testing.T) {
	for _, testCase := range allGoldenCases() {
		checkOrUpdateGolden(t, testCase)
	}
}

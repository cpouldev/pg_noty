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

// The corpus axes, as literals: operation {insert, update, delete} = 3, mode
// {full, full + exclude, columns, keys_only} = 4 and include_old {false, true} = 2, so the grid is
// 3 x 4 x 2 = 24 cells, beside the 3 named goldens the axes do not reach. The pin reads the axis
// values gridCases ranges over and the files committed under testdata/ against these numbers, so
// dropping a mode shrinks one side of a comparison and not the other -- unlike a product computed
// from the very slices the rows are built from, which moves together with any contents at all.
const (
	declaredOperations   = 3
	declaredModes        = 4
	declaredIncludeOld   = 2
	declaredGridCells    = declaredOperations * declaredModes * declaredIncludeOld
	declaredNamedGoldens = 3
)

// goldenPopulation holds the three populations the corpus pin reconciles. They move independently:
// the axis sizes are the declarations, the asked-for names are what the crossing builds, and the
// committed names are what is on disk.
type goldenPopulation struct {
	operations, modes, includeOld int
	rows, named, committed        []string
}

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
	return []goldenCase{
		{name: "named_when_update.golden", request: when},
		{name: "named_columns_update.golden", request: columns},
		{name: "named_hostile_catalog_table.golden", request: hostile},
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

// reportGoldenPopulationIssues drives goldenPopulationIssues -- the same function
// TestTheCorpusPinFailsOnADroppedCellOrAxisValue falsifies, rather than a second expression of the
// pin.
func reportGoldenPopulationIssues(t *testing.T) {
	t.Helper()
	for _, issue := range goldenPopulationIssues(observedGoldenPopulation(t)) {
		t.Error(issue)
	}
}

func TestGoldenCorpusIsTheCrossedProduct(t *testing.T) {
	if !*updateGoldens {
		reportGoldenPopulationIssues(t)
	}
	for _, testCase := range allGoldenCases() {
		checkOrUpdateGolden(t, testCase)
	}
	if *updateGoldens {
		// A -update run is held to the same shape it just wrote, so a cell the crossing stopped
		// asking for is left behind on disk rather than silently leaving the corpus.
		reportGoldenPopulationIssues(t)
	}
}

func TestGoldenCorpusDoesNotWriteWithoutUpdate(t *testing.T) {
	if *updateGoldens {
		t.Skip("mtime proof applies to the non-update run")
	}
	before := goldenMtimes(t)
	for _, testCase := range allGoldenCases() {
		checkOrUpdateGolden(t, testCase)
	}
	after := goldenMtimes(t)
	for path, stamp := range before {
		if after[path] != stamp {
			t.Errorf("%s changed without -update", path)
		}
	}
}

func goldenMtimes(t *testing.T) map[string]int64 {
	t.Helper()
	stamps := make(map[string]int64)
	for _, testCase := range allGoldenCases() {
		info, err := os.Stat(goldenPath(testCase.name))
		if err != nil {
			t.Fatalf("stat %s: %v", testCase.name, err)
		}
		stamps[testCase.name] = info.ModTime().UnixNano()
	}
	return stamps
}

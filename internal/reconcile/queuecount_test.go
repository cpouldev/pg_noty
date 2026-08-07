package reconcile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestQueueCountIsTheOnlyBoundedQueueReader(t *testing.T) {
	issues := queueSourceIssues(queueSourceFiles(t))
	if len(issues) != 0 {
		t.Fatalf("queue boundary issues: %v", issues)
	}
	if got := len(queueSourceFiles(t)); got == 0 {
		t.Fatal("queue boundary examined no reconcile production sources")
	}
}

func TestQueueBoundaryScanRefusesAForbiddenWrite(t *testing.T) {
	// Drive the same scanner predicate against a bad source.
	for _, forbidden := range []string{
		"UP" + "DATE", "DE" + "LETE", "IN" + "SERT", "FOR UP" + "DATE", "SKIP LO" + "CKED", "pay" + "load",
	} {
		t.Run(
			forbidden, func(t *testing.T) {
				issues := queueSourceIssues(map[string]string{"queuecount.go": "SELECT " + schema.TableEventQueue + " " + forbidden})
				if len(issues) == 0 {
					t.Fatal("queue boundary accepted its forbidden control")
				}
			},
		)
	}
}

func TestQueueBoundaryScanRefusesAnAliasedSchemaSelector(t *testing.T) {
	issues := queueSourceIssues(
		map[string]string{
			"other.go":      "package reconcile\nimport queueSchema \"github.com/cpouldev/pg_noty/internal/schema\"\nvar _ = queueSchema.TableEventQueue\n",
			"queuecount.go": "package reconcile",
		},
	)
	if len(issues) == 0 {
		t.Fatal("queue boundary accepted an aliased schema queue selector")
	}
}

func TestLiveQueueDefinitionComesFromTheMigration(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "schema", "migrations", "0002_objects.sql"))
	if err != nil {
		t.Fatal(err)
	}
	words := strings.Join(strings.Fields(string(data)), " ")
	words = strings.ReplaceAll(strings.ReplaceAll(words, "\"", ""), "-- ", "")
	if !strings.Contains(words, "live"+" is narrowed to pending and delivering") {
		t.Fatal("migration no longer defines live as pending and delivering")
	}
}

func TestRegistryUsesNamingAuthoritiesAndItsOwnRowType(t *testing.T) {
	data, err := os.ReadFile("registry.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, authority := range []string{
		"schema.TableListeners", "schema.TableListenerTriggers", "schema.Qualified", "source.SplitQualified",
	} {
		if !strings.Contains(source, authority) {
			t.Fatalf("registry.go does not use %s", authority)
		}
	}
	if strings.Contains(source, "Registry"+"Row") {
		t.Fatal("registry.go must return its own registry row type")
	}
	for _, spelled := range []string{"listen" + "ers", "listener_" + "triggers"} {
		if strings.Contains(source, spelled) {
			t.Fatalf("registry.go spells table name %q instead of using schema authority", spelled)
		}
	}
}

func queueSourceFiles(t *testing.T) map[string]string {
	t.Helper()
	// Count the scan population before asserting its content: an empty scan cannot look
	// clean.
	files := make(map[string]string)
	for _, name := range productionSourceNames(t) {
		files[name] = string(sourceBytes(t, name))
	}
	t.Logf("queue boundary examined %d reconcile production sources", len(files))
	return files
}

func queueSourceIssues(files map[string]string) []string {
	issues := make([]string, 0)
	if len(files) == 0 {
		return append(issues, "no production sources")
	}
	for name, data := range files {
		mentionsQueue := strings.Contains(data, schema.TableEventQueue) || namesQueueTableConstant(name, data)
		if name != "queuecount.go" && mentionsQueue {
			issues = append(issues, name+" reads the queue")
		}
	}
	data, ok := files["queuecount.go"]
	if !ok {
		return append(issues, "missing bounded queue reader")
	}
	for _, forbidden := range []string{"UPDATE", "DELETE", "INSERT", "FOR UPDATE", "SKIP LOCKED"} {
		if strings.Contains(strings.ToUpper(data), forbidden) {
			issues = append(issues, "queue reader contains "+forbidden)
		}
	}
	if strings.Contains(strings.ToLower(data), "payload") {
		issues = append(issues, "queue reader contains payload")
	}
	return issues
}

func namesQueueTableConstant(name, source string) bool {
	file := parsedSourceForQueueScan(name, source)
	if file == nil {
		return false
	}
	mentioned := false
	ast.Inspect(
		file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "TableEventQueue" {
				mentioned = true
			}
			return true
		},
	)
	return mentioned
}

func parsedSourceForQueueScan(name, source string) *ast.File {
	file, err := parser.ParseFile(token.NewFileSet(), name, source, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	return file
}

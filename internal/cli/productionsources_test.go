package cli

import (
	"path/filepath"
	"testing"

	"github.com/cpouldev/pg_noty/internal/goartifact"
)

// productionPaths is every non-test source this package and the binary hold. The import allow-list,
// the redaction scan and the single-site scans all range over it.
func productionPaths(t *testing.T) []string {
	t.Helper()
	own, err := goartifact.ProductionSources(".")
	if err != nil {
		t.Fatal(err)
	}
	main, err := goartifact.ProductionSources(filepath.Join("..", "..", "cmd", "pg_noty"))
	if err != nil {
		t.Fatal(err)
	}
	return append(own, main...)
}

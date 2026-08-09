package goartifact

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ProductionSources returns the sorted Go sources in dir that are not test files.
// It is a population measurement helper: callers retain ownership of package policy,
// floors, and any interpretation of the returned paths.
func ProductionSources(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read Go source directory %q: %w", dir, err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("Go source directory %q has no production sources", dir)
	}
	slices.Sort(paths)
	return paths, nil
}

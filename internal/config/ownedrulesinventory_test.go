package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// fixtureNamesIn returns the base names of the immediate YAML children of one corpus directory.
func fixtureNamesIn(t *testing.T, dir string) []string {
	t.Helper()
	paths := fixturesIn(t, dir)
	if len(paths) == 0 {
		t.Fatalf("%s holds no fixtures, so this assertion would pass vacuously", dir)
	}
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	return names
}

func hasPrefixed(names []string, prefix string) bool {
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func anyCarries(diags Errors, rule RuleID) bool {
	for _, diag := range diags {
		if diag.Rule == rule {
			return true
		}
	}
	return false
}

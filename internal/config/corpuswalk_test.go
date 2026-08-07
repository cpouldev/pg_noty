package config

import (
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
)

func recursiveYAMLFixtures(t *testing.T, roots ...string) []string {
	t.Helper()
	var fixtures []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && filepath.Ext(path) == fixtureExtension {
				fixtures = append(fixtures, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	slices.Sort(fixtures)
	if len(fixtures) == 0 {
		t.Fatal("recursive YAML fixture inventory is empty")
	}
	return fixtures
}

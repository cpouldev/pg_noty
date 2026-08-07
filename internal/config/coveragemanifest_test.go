package config

import (
	"io/fs"
	"path/filepath"
	"testing"
)

const coverageManifestExtent = 12

func TestTheCorpusManifestExtentIsExact(t *testing.T) {
	count := 0
	err := filepath.WalkDir("testdata", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && filepath.Ext(path) == ".rules" {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking manifests failed: %v", err)
	}
	if count != coverageManifestExtent {
		t.Errorf("corpus holds %d .rules manifests, want exactly %d", count, coverageManifestExtent)
	}
}

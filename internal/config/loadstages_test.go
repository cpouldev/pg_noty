package config

import (
	"path/filepath"
	"testing"
)

// TestLoadAcceptsAMappingRootFromTestdata is the end-to-end half of the pipeline's contract: a
// committed valid fixture reaches stage I and comes back as a configuration, with nothing reported.
func TestLoadAcceptsAMappingRootFromTestdata(t *testing.T) {
	path := filepath.Join("testdata", "valid", "full.yaml")
	cfg, warnings, errs := Load(path, corpusEnvironment())
	if len(errs) != 0 {
		t.Fatalf("Load(%s) returned diagnostics: %+v", path, errs)
	}
	if len(warnings) != 0 {
		t.Errorf("Load(%s) returned warnings: %+v", path, warnings)
	}
	if cfg == nil {
		t.Error("Load() returned no configuration after stage I")
	}
}

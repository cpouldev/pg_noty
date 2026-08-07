package goartifact

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestProductionSourcesReturnsSortedProductionFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"z.go", "a.go", "ignored_test.go", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package sample\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ProductionSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a.go"), filepath.Join(dir, "z.go")}
	if !slices.Equal(got, want) {
		t.Fatalf("ProductionSources = %v, want %v", got, want)
	}
}

func TestProductionSourcesRefusesMissingOrEmptyDirectories(t *testing.T) {
	if _, err := ProductionSources(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing directory was accepted")
	}
	if _, err := ProductionSources(t.TempDir()); err == nil {
		t.Fatal("empty directory was accepted")
	}
}

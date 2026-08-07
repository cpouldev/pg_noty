package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func coveredFixtures(t *testing.T) []string {
	t.Helper()
	return append(fixturesIn(t, invalidCorpus), absentFixture(), directoryFixture())
}

func renderCorpus(t *testing.T) []string {
	t.Helper()
	covered := coveredFixtures(t)
	for _, path := range covered {
		t.Run(filepath.Base(path), func(t *testing.T) {
			compareGolden(t, goldenFor(path), diagnosticsOf(t, path).Render(sourceBytesOf(path)))
		})
	}
	return covered
}

func diagnosticsOf(t *testing.T, path string) Errors {
	t.Helper()
	if data, err := os.ReadFile(path); err == nil {
		_, _, errs := Parse(data, filepath.Base(path), MapEnv(nil))
		return errs
	}
	_, _, errs := Load(path, MapEnv(nil))
	return errs
}

func sourceBytesOf(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

func compareGolden(t *testing.T, golden, got string) {
	t.Helper()
	if *updateGoldens {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s failed: %v", golden, err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatal(missingGoldenMessage(golden, got))
	}
	if err != nil {
		t.Fatalf("reading %s failed: %v", golden, err)
	}
	if got != string(want) {
		t.Errorf("rendered output does not match %s:\n%s",
			golden, goldenDiff(string(want), got))
	}
}

func fixturesIn(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*"+fixtureExtension))
	if err != nil {
		t.Fatalf("globbing %s failed: %v", dir, err)
	}
	slices.Sort(paths)
	return paths
}

func goldenFor(fixture string) string {
	return strings.TrimSuffix(fixture, fixtureExtension) + goldenExtension
}

func fixture(name string) string {
	return filepath.Join(invalidCorpus, name+fixtureExtension)
}

func absentFixture() string {
	return filepath.Join(unreadableCorpus, "absent"+fixtureExtension)
}

func directoryFixture() string {
	return filepath.Join(unreadableCorpus, "a_directory"+fixtureExtension)
}

func readFixtureBytes(t testing.TB, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s failed: %v", path, err)
	}
	return data
}

func readGolden(t *testing.T, path string) string {
	t.Helper()
	return string(readFixtureBytes(t, path))
}

func goldenPaths(t *testing.T) []string {
	t.Helper()
	var paths []string
	for _, dir := range []string{invalidCorpus, unreadableCorpus, validCorpus} {
		found, err := filepath.Glob(filepath.Join(dir, "*"+goldenExtension))
		if err != nil {
			t.Fatalf("globbing %s failed: %v", dir, err)
		}
		paths = append(paths, found...)
	}
	slices.Sort(paths)
	return paths
}

func goldenTimestamps(t *testing.T) map[string]time.Time {
	t.Helper()
	stamps := make(map[string]time.Time)
	for _, path := range goldenPaths(t) {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s failed: %v", path, err)
		}
		stamps[path] = info.ModTime()
	}
	return stamps
}

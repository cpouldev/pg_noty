package config

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidFixturesRenderNothing(t *testing.T) {
	fixtures := fixturesIn(t, validCorpus)
	if len(fixtures) == 0 {
		t.Fatal("the valid corpus is empty, so this test would pass vacuously")
	}
	for _, path := range fixtures {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading %s failed: %v", path, err)
			}
			_, warnings, errs := Parse(data, filepath.Base(path), corpusEnvironment())
			if len(errs) != 0 {
				t.Errorf("Parse() returned %d diagnostics, want none: %+v", len(errs), errs)
			}
			if rendered := errs.Render(data) + warnings.Render(data); rendered != "" {
				t.Errorf("rendered output is %q, want the empty string", rendered)
			}
		})
	}
}

func TestNoGoldenCarriesAControlByte(t *testing.T) {
	goldens := goldenPaths(t)
	if len(goldens) == 0 {
		t.Fatal("no golden files found, so this test would pass vacuously")
	}
	for _, path := range goldens {
		t.Run(filepath.Base(path), func(t *testing.T) {
			for at, char := range []byte(readGolden(t, path)) {
				if char >= 0x20 && char != 0x7f {
					continue
				}
				if char == '\n' || char == '\t' {
					continue
				}
				t.Errorf("byte %d is the control character %#02x; a golden may carry none but its own newlines and a tab a message quotes",
					at, char)
			}
		})
	}
}

func TestAMissingGoldenNamesTheCommandThatCreatesIt(t *testing.T) {
	const golden = "testdata/invalid/not_recorded_yet.golden"
	const rendered = "listeners.yaml:1:1\n>  1 | version: 1\n       ^ message\n"
	message := missingGoldenMessage(golden, rendered)
	for _, want := range []string{golden, rendered, "-" + goldenUpdateFlag} {
		if !strings.Contains(message, want) {
			t.Errorf("the missing-golden message does not name %q:\n%s", want, message)
		}
	}
	if flag.Lookup(goldenUpdateFlag) == nil {
		t.Errorf("the message names -%s, which the harness does not register", goldenUpdateFlag)
	}
}

func TestEveryGoldenHasAFixtureThatRendersIt(t *testing.T) {
	rendered := make(map[string]bool)
	for _, path := range coveredFixtures(t) {
		rendered[goldenFor(path)] = true
	}
	goldens := goldenPaths(t)
	if len(goldens) == 0 {
		t.Fatal("no golden files found, so this test would pass vacuously")
	}
	for _, path := range goldens {
		if !rendered[path] {
			t.Errorf("%s is rendered by no fixture; a golden nothing renders can never fail", path)
		}
	}
}

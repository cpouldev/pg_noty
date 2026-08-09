package config

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/goccy/go-yaml/ast"
)

// panickingWrapper makes wrapperReading's panic attribution reachable by a named test.
type panickingWrapper struct{ presence }

func (p *panickingWrapper) UnmarshalYAML(ast.Node) error {
	panic("a value this wrapper could not survive")
}

func TestTheFuzzGuardReportsAPanicRatherThanLettingItCrashTheRun(t *testing.T) {
	src, node := nodeAt(t, "value: x\n", "$.value")
	failed, panicked := wrapperReading(&panickingWrapper{}, node, newDecodePass(src))
	if panicked == nil {
		t.Fatal("wrapperReading did not recover the wrapper panic")
	}
	if failed != nil {
		t.Errorf("failed = %v, want nil: the wrapper panicked rather than answered", failed)
	}
}

func TestTheFuzzSeedCorpusCoversEveryClassItClaims(t *testing.T) {
	classes := map[string]func(string) bool{
		"an integer":       func(seed string) bool { _, ok := asInt(seed); return ok },
		"a boolean":        func(seed string) bool { _, ok := asBool(seed); return ok },
		"a duration":       func(seed string) bool { _, ok := asDuration(seed); return ok },
		"a null":           func(seed string) bool { return seed == "null" || seed == "~" },
		"the empty string": func(seed string) bool { return seed == "" },
		"plain text": func(seed string) bool {
			_, isInt := asInt(seed)
			_, isBool := asBool(seed)
			_, isDur := asDuration(seed)
			return seed != "" && !isInt && !isBool && !isDur &&
				utf8.ValidString(seed) && !strings.ContainsAny(seed, "{}[]:#\t\n\r\x00")
		},
		"a list":                  func(seed string) bool { return strings.HasPrefix(seed, "[") },
		"a mapping":               func(seed string) bool { return strings.HasPrefix(seed, "{") },
		"a node property":         func(seed string) bool { return strings.HasPrefix(seed, "!!") },
		"bytes that are not text": func(seed string) bool { return strings.Contains(seed, "\x00") },
		"a multi-line value":      func(seed string) bool { return strings.Contains(seed, "\n") },
		"grammar-breaking bytes":  func(seed string) bool { return strings.ContainsAny(seed, "\x00\n\r") },
	}
	for class, matches := range classes {
		t.Run(class, func(t *testing.T) {
			for _, seed := range fuzzSeeds {
				if matches(seed) {
					return
				}
			}
			t.Errorf("no seed covers %s", class)
		})
	}
}

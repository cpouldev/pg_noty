package config

import (
	"slices"
	"strings"
	"testing"
)

// This file asserts the shape table against the configuration contract. Its subject is a
// deliverable of its own, so it has its own test file, following this package's one file
// per subject convention.

// TestEveryMappingLevelOfTheContractIsDeclared walks to each level the specification
// enumerates and asserts its legal keys and its free-form flag. Reaching a level by
// walking, rather than by naming a table entry, is what proves the level is reachable at
// that path: a child pointer left dangling fails here even though the level itself exists.
//
// Two levels are one declaration reached from two paths, because they are one shape:
// `retry` (under defaults and under a listener, which is what lets a listener override one
// retry field and inherit the rest) and `headers` (under defaults and under a
// destination). The specification lists `defaults.headers` and `destination.headers`
// separately and `retry` once; both paths of both shapes are asserted below, so the
// enumeration is covered either way round.
func TestEveryMappingLevelOfTheContractIsDeclared(t *testing.T) {
	for _, tc := range mappingLevelCases() {
		t.Run(tc.level, func(t *testing.T) {
			level := levelAt(t, tc.path...)

			if got := keyNames(level); !slices.Equal(got, tc.keys) {
				t.Errorf("declared keys %v, want %v", got, tc.keys)
			}
			if level.freeForm != tc.freeForm {
				t.Errorf("freeForm = %t, want %t", level.freeForm, tc.freeForm)
			}
		})
	}
}

// TestEveryRemovedKeyCarriesItsMigrationHint covers AC #10: the four keys the corrected
// contract moved or removed each name where they went. The wanted substring is the new
// location or shape, so a hint that merely says the key is unknown fails the row.
func TestEveryRemovedKeyCarriesItsMigrationHint(t *testing.T) {
	tests := []struct {
		name     string
		path     []string
		key      string
		wantHint string
	}{
		{
			name:     "destination.type",
			path:     []string{"listeners", "destination"},
			key:      "type",
			wantHint: "HTTP",
		},
		{
			name:     "listener-level when",
			path:     []string{"listeners"},
			key:      "when",
			wantHint: "operations.<op>.when",
		},
		{
			name:     "listener-level columns",
			path:     []string{"listeners"},
			key:      "columns",
			wantHint: "operations.update.columns",
		},
		{
			name:     "signing.secret",
			path:     []string{"listeners", "destination", "signing"},
			key:      "secret",
			wantHint: "signing.secrets",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			level := levelAt(t, tc.path...)

			hint, removed := level.removedKey(tc.key)
			if !removed {
				t.Fatalf("%q is not declared as a removed key", tc.key)
			}
			if !strings.Contains(hint, tc.wantHint) {
				t.Errorf("hint %q does not name %q", hint, tc.wantHint)
			}
		})
	}
}

// TestARemovedKeyIsNotAlsoALegalKey covers the other side of the removed-key table: a key
// that is both declared and removed would be accepted by the unknown-key check and never
// reach its migration hint.
func TestARemovedKeyIsNotAlsoALegalKey(t *testing.T) {
	for name, level := range schemaLevels {
		for _, removed := range level.removed {
			if _, legal := level.key(removed.name); legal {
				t.Errorf("level %q declares %q as both a legal key and a removed one", name, removed.name)
			}
		}
	}
}

// levelAt resolves a mapping level by walking the declared table from the root, so an
// assertion proves the level is reachable at that path rather than merely declared
// somewhere. A sequence of mappings is transparent: `listeners` walks straight into the
// listener level, because a key names one shape whether its value holds that shape or a
// list of it.
func levelAt(t *testing.T, path ...string) mappingLevel {
	t.Helper()

	level := schemaLevels[levelRoot]
	for i, name := range path {
		spec, declared := level.key(name)
		if !declared {
			t.Fatalf("no key %q under %q", name, strings.Join(path[:i], "."))
		}
		child, declared := schemaLevels[spec.child]
		if !declared {
			t.Fatalf("key %q under %q declares no mapping level", name, strings.Join(path[:i], "."))
		}
		level = child
	}
	return level
}

// keyNames reads the level's own answer rather than walking its keys a second time;
// the suggestion walk draws its candidates from the same one.
func keyNames(level mappingLevel) []string { return level.declaredNames() }

// declaredKeys is every key of the table by its full path, so an assertion about a whole
// flag class is made over the table rather than over the levels a test remembered.
func declaredKeys(t *testing.T) map[string]keySpec {
	t.Helper()

	found := make(map[string]keySpec)
	collectDeclaredKeys(t, schemaLevels[levelRoot], "", nil, found)
	return found
}

func collectDeclaredKeys(t *testing.T, level mappingLevel, prefix string, ancestors []levelName, found map[string]keySpec) {
	t.Helper()

	for _, spec := range level.keys {
		path := prefix + spec.name
		found[path] = spec

		child, declared := schemaLevels[spec.child]
		if !declared {
			continue
		}
		if slices.Contains(ancestors, spec.child) {
			// Without this the walk would not terminate, and a hang says far less than a
			// named failure about a table that has become cyclic.
			t.Fatalf("level %q is reachable from itself through %q", spec.child, path)
		}
		// A key whose value is only ever a sequence reaches its child shape through the
		// sequence's elements, which `[]` marks. A key accepting either shape -- the
		// operations list-form sugar -- reaches it directly in the mapping form, and the
		// list form holds scalars rather than that shape at all.
		if spec.kinds == sequenceValue {
			path += "[]"
		}
		collectDeclaredKeys(t, child, path+".", append(ancestors, spec.child), found)
	}
}

// pathsWhere is the sorted paths of every declared key the predicate accepts.
func pathsWhere(t *testing.T, accepts func(keySpec) bool) []string {
	t.Helper()

	var paths []string
	for path, spec := range declaredKeys(t) {
		if accepts(spec) {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths
}

func leafOf(path string) string {
	if at := strings.LastIndexByte(path, '.'); at >= 0 {
		return path[at+1:]
	}
	return path
}

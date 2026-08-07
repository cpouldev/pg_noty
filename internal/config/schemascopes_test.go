package config

import (
	"slices"
	"testing"
)

// What the shape table says about a *value* and about the mapping that holds it: the YAML shapes a
// key accepts, how much of its value must never be rendered, the shape declared beneath it, and the
// three facts a level carries as a whole -- which mappings hold HTTP field names, which of them a
// missing required key is anchored on, and which accept any name at all.
//
// Which *rule* the table names for each way a key can be got wrong is schemarules_test.go's; which
// keys and levels it declares in the first place is schema_test.go's.
//
// Every fact here is read by stage F as data rather than decided in code, so each is asserted over
// the whole table rather than over the levels a test remembered to visit: a level or key acquiring
// one of them by accident fails by name.

// TestTheContractDeclaresTwoAnchorScopes pins ADR-6's class for a missing required key as data.
// The scopes are where a missing key's caret lands, so a level gaining or losing one silently
// moves every such caret in the document (AC #30, CK-5).
func TestTheContractDeclaresTwoAnchorScopes(t *testing.T) {
	var scopes []string
	for name, level := range schemaLevels {
		if level.scope {
			scopes = append(scopes, string(name))
		}
	}
	slices.Sort(scopes)

	if want := []string{string(levelListener), string(levelRoot)}; !slices.Equal(scopes, want) {
		t.Errorf("anchor scopes %v, want exactly %v: the document root and a listener", scopes, want)
	}
}

// TestOnlyTheHeaderMappingHoldsHttpFieldNames scopes R20 to the level whose names are HTTP field
// names. Sweeping another level in would make two of its keys differing only in case one key,
// which is true of a header and of nothing else the contract declares.
func TestOnlyTheHeaderMappingHoldsHttpFieldNames(t *testing.T) {
	for name, level := range schemaLevels {
		if wantsCaseFolding := name == levelHeaders; level.httpFieldNames != wantsCaseFolding {
			t.Errorf("level %q declares httpFieldNames = %t, want %t", name, level.httpFieldNames, wantsCaseFolding)
		}
	}
}

// TestTheOnlyShapeHintIsTheOneShowingBothOperationForms scopes the wrong-shape hint to the key
// that needs it. AC #16 requires the bare-scalar diagnostic to show both accepted forms, and no
// other key accepts two shapes at all -- so a hint appearing elsewhere would be spelling out a
// shape a generic message already names.
func TestTheOnlyShapeHintIsTheOneShowingBothOperationForms(t *testing.T) {
	for path, spec := range declaredKeys(t) {
		if hinted := spec.shapeHint != ""; hinted != (leafOf(path) == operationsKey) {
			t.Errorf("%s declares shapeHint %q, want one only for %q", path, spec.shapeHint, operationsKey)
		}
	}
	if got := declaredKeys(t)["listeners[].operations"].shapeHint; got != bothOperationForms {
		t.Errorf("the operations shape hint is %q, want the one spelling both forms", got)
	}
}

// TestOnlyDiagnosticSecretPathsAreSensitive is D3's complete schema-owned render set.
// destination.url is deliberately public on this surface even when it carries credentials.
func TestOnlyDiagnosticSecretPathsAreSensitive(t *testing.T) {
	want := []string{
		"database.listen_url",
		"database.url",
		"listeners[].destination.signing.secrets",
	}

	got := pathsWhere(t, func(spec keySpec) bool { return spec.sensitive != publicValue })

	if !slices.Equal(got, want) {
		t.Errorf("sensitive keys %v, want exactly %v", got, want)
	}
}

// TestSensitiveValuesDeclareHowMuchOfThemGoes separates the two extents D3 defines. A URL
// blanked wholesale is a useless diagnostic; a secret blanked in part is a leak.
func TestSensitiveValuesDeclareHowMuchOfThemGoes(t *testing.T) {
	declared := declaredKeys(t)

	tests := []struct {
		path string
		want sensitivity
	}{
		{path: "database.url", want: urlPassword},
		{path: "database.listen_url", want: urlPassword},
		{path: "listeners[].destination.signing.secrets", want: entireValue},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if got := declared[tc.path].sensitive; got != tc.want {
				t.Errorf("sensitivity = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestTheFallbackKeyNamesComeFromTheTable asserts that the key-name set D3's fallback
// matches on is derived from the same declarations rather than written out a second time.
// Both sides are computed from the table, so a new sensitive key updates both at once --
// which is the property being asserted; the table's own contents are pinned above.
func TestTheFallbackKeyNamesComeFromTheTable(t *testing.T) {
	var want []string
	for path, spec := range declaredKeys(t) {
		if spec.sensitive != publicValue {
			want = append(want, leafOf(path))
		}
	}
	slices.Sort(want)
	want = slices.Compact(want)

	if !slices.Equal(sensitiveKeyNames, want) {
		t.Errorf("sensitiveKeyNames = %v, want %v", sensitiveKeyNames, want)
	}
}

// TestNodeKindsAreDeclaredPreciselyEnoughToCheck covers the kinds a wrong-shape check needs
// from this table, including the one key that legitimately accepts two shapes: the
// operations list-form sugar.
func TestNodeKindsAreDeclaredPreciselyEnoughToCheck(t *testing.T) {
	declared := declaredKeys(t)

	tests := []struct {
		path string
		want nodeKinds
	}{
		{path: "version", want: scalarValue},
		{path: "database", want: mappingValue},
		{path: "listeners", want: sequenceValue},
		{path: "listeners[].operations", want: mappingValue | sequenceValue},
		{path: "listeners[].operations.update.columns", want: sequenceValue},
		{path: "listeners[].payload.mode", want: scalarValue},
		{path: "listeners[].destination.signing.secrets", want: sequenceValue},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if got := declared[tc.path].kinds; got != tc.want {
				t.Errorf("kinds = %b, want %b", got, tc.want)
			}
		})
	}
}

// TestEveryDeclaredChildLevelExists rules out the failure the walk above cannot see: a key
// naming a level the table does not declare would silently hold no shape at all, so an
// unknown-key check below it would accept anything.
func TestEveryDeclaredChildLevelExists(t *testing.T) {
	for name, level := range schemaLevels {
		for _, spec := range level.keys {
			if spec.child == noLevel {
				continue
			}
			if _, declared := schemaLevels[spec.child]; !declared {
				t.Errorf("key %q of level %q names undeclared level %q", spec.name, name, spec.child)
			}
		}
	}
}

// TestAFreeFormLevelDeclaresNoKeys covers both sides of the free-form flag: a header
// mapping accepts any name, so declaring keys there would make some names more legal than
// others, and a level with keys must not be free-form or its keys would never be checked.
func TestAFreeFormLevelDeclaresNoKeys(t *testing.T) {
	for name, level := range schemaLevels {
		if level.freeForm && len(level.keys) != 0 {
			t.Errorf("level %q is free-form yet declares %d keys", name, len(level.keys))
		}
	}
}

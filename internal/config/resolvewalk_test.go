package config

import (
	"slices"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// Whether stage G's resolution reaches every wrapper it decoded.
//
// The failure this guards against is silent, which is why it has a test of its own: a raw field the walk
// does not reach decodes correctly, records nothing, and answers line zero -- so the first thing anyone
// notices is a Step-8 diagnostic pointing at the top of the file.

// TestEveryWrapperOfADecodedDocumentIsResolved is what keeps the resolution walk complete. A raw field
// the walk cannot reach would carry no position, and nothing else would fail: the value would decode,
// the run would report nothing, and Step 8's diagnostic about that field would point at line zero.
//
// It walks the decoded tree the same way the production walk does, so a field added to raw.go in a
// place the walk does not descend fails here by name.
func TestEveryWrapperOfADecodedDocumentIsResolved(t *testing.T) {
	decoded := decodedConfig(t, everyKindOfValue("  concurrency: 8\n"))

	unresolved := unresolvedWrappersIn(t, decoded)
	if len(unresolved) != 0 {
		t.Errorf("%d wrappers carry no position: %v", len(unresolved), unresolved)
	}
	// Without this the assertion would hold of a tree the walk never entered, and a floor is not enough:
	// a floor of five was satisfied by a walk that stopped after the root mapping, and a floor of any
	// number leaves slack for exactly as many fields as the slack is wide. What the walk reaches is
	// therefore an equality against the tree's own field count, derived below rather than written here.
	reached := resolvedWrapperCount(t, decoded)
	if want := wrappersBeneathTheRawTree(); reached != want {
		t.Fatalf("%d wrappers were reached, want the %d the raw tree declares for this document; the walk "+
			"stops short, so the assertion above covers whatever it did not enter", reached, want)
	}
}

// TestTheResolutionDocumentWritesEveryDeclaredKey is what makes the test above cover the contract rather
// than the keys its author happened to type.
//
// It was not covering it: the document omitted eleven declared keys, so eleven raw fields could be left
// unresolved with the suite green -- and an unresolved wrapper's recorded refusal is never reported at
// all, which is the same silent under-reporting as a collapsed position. The list is read from the schema
// table so a key added later joins it without anyone remembering.
func TestTheResolutionDocumentWritesEveryDeclaredKey(t *testing.T) {
	document := everyKindOfValue("  concurrency: 8\n")
	want := declaredResolutionPaths(levelRoot, "")
	got := writtenResolutionPaths(t, document)

	slices.Sort(want)
	slices.Sort(got)
	want = slices.Compact(want)
	got = slices.Compact(got)
	if !slices.Equal(got, want) {
		t.Errorf("the resolution document writes\n%v\nwant every level-qualified declaration\n%v",
			got, want)
	}
}

// declaredResolutionPaths derives the complete, level-qualified fixture obligation from the
// schema tree. A retry field under defaults and the same field under a listener are distinct paths,
// so writing one can never satisfy the other.
func declaredResolutionPaths(level levelName, prefix string) []string {
	var paths []string
	for _, spec := range schemaLevels[level].keys {
		path := joinedResolutionPath(prefix, spec.name)
		paths = append(paths, path)

		if _, hasChild := schemaLevels[spec.child]; !hasChild {
			continue
		}
		childPrefix := path
		if level == levelOperations {
			childPrefix = prefix + "[]"
		} else if spec.kinds == sequenceValue {
			childPrefix += "[]"
		}
		paths = append(paths, declaredResolutionPaths(spec.child, childPrefix)...)
	}
	return paths
}

func writtenResolutionPaths(t *testing.T, document string) []string {
	t.Helper()

	_, root, diags := stageE(t, document, corpusVariables)
	if len(diags) != 0 {
		t.Fatalf("the resolution document does not reach the structural boundary: %q", messagesOf(diags))
	}

	var paths []string
	collectWrittenResolutionPaths(root, levelRoot, "", &paths)
	return paths
}

func collectWrittenResolutionPaths(node ast.Node, level levelName, prefix string, paths *[]string) {
	mapping, isMapping := node.(*ast.MappingNode)
	if !isMapping {
		return
	}

	for _, entry := range mapping.Values {
		name, readable := keyTextOf(entry.Key)
		if !readable {
			continue
		}
		spec, declared := schemaLevels[level].key(name)
		if !declared {
			continue
		}

		path := joinedResolutionPath(prefix, name)
		*paths = append(*paths, path)
		_, hasChild := schemaLevels[spec.child]
		if !hasChild {
			continue
		}

		childPrefix := path
		if level == levelOperations {
			childPrefix = prefix + "[]"
		} else if _, isSequence := entry.Value.(*ast.SequenceNode); isSequence && spec.kinds == sequenceValue {
			childPrefix += "[]"
		}
		for _, held := range valuesOf(entry.Value) {
			collectWrittenResolutionPaths(held, spec.child, childPrefix, paths)
		}
	}
}

func joinedResolutionPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

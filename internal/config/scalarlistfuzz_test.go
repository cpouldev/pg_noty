package config

import (
	"fmt"
	"testing"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// A StrList's input domain has two independently generated dimensions per element: the opaque bytes
// interpolation left in a scalar holder, and the AST shape the list has to visit. Shape is generated
// rather than encoded in those bytes because re-parsing opaque bytes would test YAML grammar instead of
// stage D's post-interpolation boundary.
type listElementShape uint8

const (
	scalarListElement listElementShape = iota
	sequenceListElement
	mappingListElement
	aliasListElement
	listElementShapeCount
)

var strListFuzzSeeds = []struct {
	first, second           string
	firstShape, secondShape uint8
}{
	{first: "first", second: "second"},
	{first: "\x00\n${FIRST}", second: "\r\xff${SECOND}"},
	{first: "nested one", second: "nested two", firstShape: 1, secondShape: 2},
	{first: "unused one", second: "unused two", firstShape: 3, secondShape: 3},
}

func FuzzStrListNeverFails(f *testing.F) {
	for _, seed := range strListFuzzSeeds {
		f.Add(seed.first, seed.second, seed.firstShape, seed.secondShape)
	}

	f.Fuzz(func(t *testing.T, first, second string, firstShape, secondShape uint8) {
		src, sequence, shapes := generatedStrList(t, [2]string{first, second}, [2]uint8{firstShape, secondShape})
		held, pass := &StrList{}, newDecodePass(src)

		failed, panicked := wrapperReading(held, sequence, pass)
		if panicked != nil {
			t.Fatalf("StrList panicked on %q and %q: %v", first, second, panicked)
		}
		if failed != nil {
			t.Fatalf("StrList.UnmarshalYAML returned %v; every wrapper must answer nil", failed)
		}

		assertStrListTraversal(t, held, sequence, [2]string{first, second}, shapes)
		if len(pass.diags) != len(held.refused) {
			t.Fatalf("resolved %d of %d element refusals: %q",
				len(pass.diags), len(held.refused), messagesOf(pass.diags))
		}
		if at, shared := positionSharedByTwo(pass.diags); shared {
			t.Fatalf("two element refusals share %s (%q); each element must keep its own position",
				at, messagesOf(pass.diags))
		}
	})
}

// assertStrListTraversal is the branch sentinel: every parsed sequence element must appear either as
// a retained Str with its exact opaque bytes or as a refusal anchored on that exact AST node.
func assertStrListTraversal(
	t testing.TB,
	held *StrList,
	sequence *ast.SequenceNode,
	written [2]string,
	shapes [2]listElementShape,
) {
	t.Helper()

	if len(sequence.Values) != len(written) {
		t.Fatalf("generated %d sequence elements, want %d", len(sequence.Values), len(written))
	}
	read, refused := 0, 0
	for i, shape := range shapes {
		if shape == scalarListElement {
			if read >= len(held.values) {
				t.Fatalf("element %d was not retained; the scalar traversal branch did not run", i)
			}
			if got := held.values[read].value; got != written[i] {
				t.Fatalf("element %d retained %q, want opaque bytes %q", i, got, written[i])
			}
			if held.values[read].node != sequence.Values[i] {
				t.Fatalf("element %d retained a different AST node", i)
			}
			read++
			continue
		}
		if refused >= len(held.refused) {
			t.Fatalf("element %d was not refused; the %s traversal branch did not run", i, shape)
		}
		if held.refused[refused].at != sequence.Values[i] {
			t.Fatalf("element %d refusal is anchored on a different AST node", i)
		}
		refused++
	}
	if read != len(held.values) || refused != len(held.refused) {
		t.Fatalf("visited %d readable and %d unreadable elements, retained %d and refused %d",
			read, refused, len(held.values), len(held.refused))
	}
}

func generatedStrList(
	t testing.TB,
	written [2]string,
	generatedShapes [2]uint8,
) (*source, *ast.SequenceNode, [2]listElementShape) {
	t.Helper()

	shapes := [2]listElementShape{
		listElementShape(generatedShapes[0] % uint8(listElementShapeCount)),
		listElementShape(generatedShapes[1] % uint8(listElementShapeCount)),
	}
	document := "value:\n  - " + shapes[0].template("FUZZ_FIRST") +
		"\n  - " + shapes[1].template("FUZZ_SECOND") + "\n"
	file, err := parser.ParseBytes([]byte(document), parser.ParseComments)
	if err != nil {
		t.Fatalf("parse the fixed list fuzz template: %v\n%s", err, document)
	}
	mapping := file.Docs[0].Body.(*ast.MappingNode)
	sequence, isSequence := mapping.Values[0].Value.(*ast.SequenceNode)
	if !isSequence {
		t.Fatalf("the fixed list template is %T, want *ast.SequenceNode", mapping.Values[0].Value)
	}
	for i, shape := range shapes {
		shape.installOpaqueBytes(t, sequence.Values[i], written[i])
	}
	return newSource("fuzz.yaml", []byte(document)), sequence, shapes
}

func (shape listElementShape) template(placeholder string) string {
	switch shape {
	case scalarListElement:
		return placeholder
	case sequenceListElement:
		return "[" + placeholder + "]"
	case mappingListElement:
		return "{held: " + placeholder + "}"
	case aliasListElement:
		return "*FUZZ_ALIAS"
	default:
		panic(fmt.Sprintf("unrecognised generated list element shape %d", shape))
	}
}

// installOpaqueBytes mutates only parsed scalar holders. Nested containers carry such a holder so
// their descendants still cover stage D's opaque-byte boundary; an alias has no interpolated holder
// and exists solely to generate StrList's otherwise-unreadable alias branch.
func (shape listElementShape) installOpaqueBytes(t testing.TB, node ast.Node, written string) {
	t.Helper()

	var holder ast.Node
	switch shape {
	case scalarListElement:
		holder = node
	case sequenceListElement:
		holder = node.(*ast.SequenceNode).Values[0]
	case mappingListElement:
		holder = node.(*ast.MappingNode).Values[0].Value
	case aliasListElement:
		if _, isAlias := node.(*ast.AliasNode); !isAlias {
			t.Fatalf("generated alias element is %T", node)
		}
		return
	}
	scalar, isString := holder.(*ast.StringNode)
	if !isString {
		t.Fatalf("generated %s holder is %T, want *ast.StringNode", shape, holder)
	}
	scalar.Value = written
}

func (shape listElementShape) String() string {
	return [...]string{"scalar", "sequence", "mapping", "alias"}[shape]
}

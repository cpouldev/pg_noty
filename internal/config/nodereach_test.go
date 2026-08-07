package config

import (
	"fmt"
	"reflect"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

// The position-stability oracle, written without reference to the stage it checks.
//
// AC #4's "every reported position is unchanged" is only as strong as the node set it is
// measured over, so the set is found here by descending the library's own tree rather than by
// asking the recursion under test which nodes exist (Implementation Note 18). A later step
// asserting position stability should reuse this rather than walking with the predicates it
// is testing.

// reportedPositions is every position the document reports, in walk order, as a diagnostic
// would carry it. It reads through positionOf so that what it compares is what a caret
// would be drawn at, rather than a coordinate no diagnostic uses.
//
// The node set is found by everyNodeReachableFrom and deliberately not by this stage's own
// valuePositionsOf and keyPositionsOf. An oracle built from the predicates under test can
// only see the nodes those predicates claim exist, so a recursion that skipped a node would
// satisfy it identically before and after -- and this is the assertion AC #4 leans on
// hardest. The independent set also holds the nodes the stage never descends, so a
// substitution that re-parsed a value and spliced the result back in is a set that no longer
// matches rather than a subtree nobody looked at.
func reportedPositions(src *source, root ast.Node) []string {
	nodes := everyNodeReachableFrom(root)
	reported := make([]string, len(nodes))

	for i, node := range nodes {
		at := positionOf(src, node)
		reported[i] = fmt.Sprintf("%T %s@%d:%d", node, at.Path(), at.Line(), at.Col())
	}
	return reported
}

// everyNodeReachableFrom is each node reachable from root through the exported fields the
// library builds its tree out of -- keys, anchor names, alias names, comments and entry
// wrappers included -- in field order, each reported once.
//
// Reflection is what makes it independent of this package. The library's own answer to the
// same question, ast.Walk, is prohibited in every file of this package including the tests
// (V1, TestParseWalksEachDocumentRatherThanTheFile), and any traversal written by hand here
// would be the recursion under test spelled a second way.
//
// Its reach has two stated limits. The descent stops at token.Token: it is not a node, and
// its Prev/Next links are the whole document's token stream rather than root's subtree. And
// the CanInterface guard skips unexported fields, which reflection cannot read without
// unsafe -- so a node this library version reached only through an unexported field would be
// outside the set, and a later step reusing this oracle inherits that bound. Every node kind
// the value and key enumerations produce is reached through exported fields today, which is
// what makes the bound affordable.
func everyNodeReachableFrom(root ast.Node) []ast.Node {
	var found []ast.Node
	visited := map[uintptr]bool{}

	var descend func(reflect.Value)
	descend = func(value reflect.Value) {
		switch value.Kind() {
		case reflect.Interface:
			if !value.IsNil() {
				descend(value.Elem())
			}

		case reflect.Pointer:
			// Shared pointers are real: a sequence holds each element in both Values and
			// Entries, so without this the same node is reported twice.
			if value.IsNil() || visited[value.Pointer()] {
				return
			}
			visited[value.Pointer()] = true
			if node, isNode := value.Interface().(ast.Node); isNode {
				found = append(found, node)
			}
			descend(value.Elem())

		case reflect.Struct:
			if value.Type() == reflect.TypeFor[token.Token]() {
				return
			}
			for i := range value.NumField() {
				if field := value.Field(i); field.CanInterface() {
					descend(field)
				}
			}

		case reflect.Slice:
			for i := range value.Len() {
				descend(value.Index(i))
			}
		}
	}
	descend(reflect.ValueOf(root))
	return found
}

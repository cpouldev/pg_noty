package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

func scalarSubject(id, path, want, replacement string) acceptanceSubject {
	return scalarSubjectUsing(id, path, want, mutateScalar(path, replacement))
}

func removableScalarSubject(id, path, want string) acceptanceSubject {
	return scalarSubjectUsing(id, path, want, removeScalar(path))
}

func scalarSubjectUsing(id, path, want string, mutate acceptanceMutation) acceptanceSubject {
	return acceptanceSubject{
		id:     id,
		source: fmt.Sprintf("%s holds %q", path, want),
		matches: func(root ast.Node) bool {
			node, exists := acceptanceNodeAt(root, path)
			if !exists {
				return false
			}
			got, scalar := acceptanceScalar(node)
			return scalar && got == want
		},
		mutate: mutate,
	}
}

func mappingKeySubject(id, path, key string) acceptanceSubject {
	return mappingKeySubjectUsing(id, path, key, removeMappingKey(path, key))
}

func mappingKeySubjectUsing(id, path, key string, mutate acceptanceMutation) acceptanceSubject {
	return acceptanceSubject{
		id:     id,
		source: fmt.Sprintf("%s contains key %q", path, key),
		matches: func(root ast.Node) bool {
			_, exists := acceptanceEntry(root, path, key)
			return exists
		},
		mutate: mutate,
	}
}

func sequenceSubject(id, path string, want []string, mutate acceptanceMutation) acceptanceSubject {
	return acceptanceSubject{
		id:     id,
		source: fmt.Sprintf("%s holds sequence %q", path, want),
		matches: func(root ast.Node) bool {
			node, exists := acceptanceNodeAt(root, path)
			sequence, isSequence := node.(*ast.SequenceNode)
			if !exists || !isSequence || len(sequence.Values) != len(want) {
				return false
			}
			got := make([]string, len(sequence.Values))
			for i, value := range sequence.Values {
				scalar, readable := acceptanceScalar(value)
				if !readable {
					return false
				}
				got[i] = scalar
			}
			return slices.Equal(got, want)
		},
		mutate: mutate,
	}
}

func configObservation(subject, state string, check func(Config) bool) acceptanceObservation {
	return acceptanceObservation{
		subject: subject, state: state, needsConfig: true,
		holds: func(evidence acceptanceEvidence) bool {
			return evidence.config != nil && check(*evidence.config)
		},
	}
}

func rawObservation(subject, state string, check func(ast.Node) bool) acceptanceObservation {
	return acceptanceObservation{
		subject: subject, state: state,
		holds: func(evidence acceptanceEvidence) bool {
			return evidence.root != nil && check(evidence.root)
		},
	}
}

func positionedKeyObservation(subject, path, key string) acceptanceObservation {
	return rawObservation(subject, path+" has a positioned "+key+" node", func(root ast.Node) bool {
		entry, exists := acceptanceEntry(root, path, key)
		return exists && realSourcePosition(entry.Key)
	})
}

func lexicalIntegerSubject(id, path, lexical, replacement string) acceptanceSubject {
	return acceptanceSubject{
		id: id, source: path + " is written lexically as " + lexical,
		matches: func(root ast.Node) bool {
			node, exists := acceptanceNodeAt(root, path)
			integer, isInteger := node.(*ast.IntegerNode)
			return exists && isInteger && integer.GetToken() != nil &&
				strings.TrimSpace(integer.GetToken().Origin) == lexical &&
				realSourcePosition(integer)
		},
		mutate: mutateScalar(path, replacement),
	}
}

func versionIntegerObservation(subject, path, lexical, value string) acceptanceObservation {
	return acceptanceObservation{
		subject: subject, state: path + " is the integer " + value, needsConfig: true,
		holds: func(evidence acceptanceEvidence) bool {
			node, exists := acceptanceNodeAt(evidence.root, path)
			integer, isInteger := node.(*ast.IntegerNode)
			if !exists || !isInteger || integer.GetToken() == nil || evidence.config == nil {
				return false
			}
			return strings.TrimSpace(integer.GetToken().Origin) == lexical &&
				fmt.Sprint(integer.Value) == value && realSourcePosition(integer) &&
				evidence.config.Version == 1
		},
	}
}

func realSourcePosition(node ast.Node) bool {
	token := node.GetToken()
	return token != nil && token.Position != nil && token.Position.Line > 0 &&
		parserReportedColumn(token) > 0
}

func parseAcceptanceRoot(data []byte) (ast.Node, error) {
	file, err := parser.ParseBytes(data, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if len(file.Docs) != 1 || file.Docs[0].Body == nil {
		return nil, fmt.Errorf("parsed %d documents, want one body", len(file.Docs))
	}
	return file.Docs[0].Body, nil
}

func acceptanceNodeAt(root ast.Node, path string) (ast.Node, bool) {
	if path == "$" {
		return root, root != nil
	}
	selector, err := yaml.PathString(path)
	if err != nil {
		return nil, false
	}
	node, err := selector.FilterNode(root)
	return node, err == nil && node != nil
}

func acceptanceEntry(root ast.Node, path, key string) (*ast.MappingValueNode, bool) {
	node, exists := acceptanceNodeAt(root, path)
	mapping, isMapping := node.(*ast.MappingNode)
	if !exists || !isMapping {
		return nil, false
	}
	for _, entry := range mapping.Values {
		if text, readable := keyTextOf(entry.Key); readable && text == key {
			return entry, true
		}
	}
	return nil, false
}

func acceptanceScalar(node ast.Node) (string, bool) {
	if scalar, holdsText := scalarTextOf(node); holdsText {
		return scalar.written(), true
	}
	switch held := node.(type) {
	case *ast.IntegerNode:
		return fmt.Sprint(held.Value), true
	case *ast.FloatNode:
		return fmt.Sprint(held.Value), true
	case *ast.BoolNode:
		return fmt.Sprint(held.Value), true
	case *ast.NullNode:
		return "null", true
	}
	return "", false
}

package config

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml/ast"
)

func mutateScalar(path, replacement string) acceptanceMutation {
	return func(data []byte) ([]byte, error) {
		root, err := parseAcceptanceRoot(data)
		if err != nil {
			return nil, err
		}
		node, exists := acceptanceNodeAt(root, path)
		if !exists {
			return nil, fmt.Errorf("cannot mutate absent scalar %s", path)
		}
		return replaceNodeToken(data, node, replacement)
	}
}

func removeScalar(path string) acceptanceMutation {
	return func(data []byte) ([]byte, error) {
		root, err := parseAcceptanceRoot(data)
		if err != nil {
			return nil, err
		}
		node, exists := acceptanceNodeAt(root, path)
		if !exists {
			return nil, fmt.Errorf("cannot remove absent scalar %s", path)
		}
		return removeNodeLine(data, node)
	}
}

func removeMappingKey(path, key string) acceptanceMutation {
	return func(data []byte) ([]byte, error) {
		root, err := parseAcceptanceRoot(data)
		if err != nil {
			return nil, err
		}
		entry, exists := acceptanceEntry(root, path, key)
		if !exists {
			return nil, fmt.Errorf("cannot remove absent key %s.%s", path, key)
		}
		return removeNodeLine(data, entry.Key)
	}
}

func replaceMappingKey(path, key, replacement string) acceptanceMutation {
	return func(data []byte) ([]byte, error) {
		root, err := parseAcceptanceRoot(data)
		if err != nil {
			return nil, err
		}
		entry, exists := acceptanceEntry(root, path, key)
		if !exists {
			return nil, fmt.Errorf("cannot replace absent key %s.%s", path, key)
		}
		return replaceNodeToken(data, entry.Key, replacement)
	}
}

func replaceText(from, to string) acceptanceMutation {
	return func(data []byte) ([]byte, error) {
		if got := bytes.Count(data, []byte(from)); got != 1 {
			return nil, fmt.Errorf("mutation source %q occurs %d times, want exactly one", from, got)
		}
		return bytes.Replace(data, []byte(from), []byte(to), 1), nil
	}
}

func combineMutations(mutations ...acceptanceMutation) acceptanceMutation {
	return func(data []byte) ([]byte, error) {
		var err error
		for _, mutate := range mutations {
			data, err = mutate(data)
			if err != nil {
				return nil, err
			}
		}
		return data, nil
	}
}

func replaceNodeToken(data []byte, node ast.Node, replacement string) ([]byte, error) {
	token := node.GetToken()
	if token == nil || token.Position == nil || token.Origin == "" {
		return nil, fmt.Errorf("%T has no replaceable source token", node)
	}
	offset, err := sourceByteOffset(data, token.Position.Line, parserReportedColumn(token))
	if err != nil {
		return nil, err
	}
	origin := []byte(strings.TrimSpace(token.Origin))
	if offset+len(origin) > len(data) || !bytes.Equal(data[offset:offset+len(origin)], origin) {
		return nil, fmt.Errorf("token %q is not present at source offset %d", origin, offset)
	}
	mutated := make([]byte, 0, len(data)-len(origin)+len(replacement))
	mutated = append(mutated, data[:offset]...)
	mutated = append(mutated, replacement...)
	mutated = append(mutated, data[offset+len(origin):]...)
	return mutated, nil
}

func removeNodeLine(data []byte, node ast.Node) ([]byte, error) {
	token := node.GetToken()
	if token == nil || token.Position == nil {
		return nil, fmt.Errorf("%T has no positioned source token", node)
	}
	offset, err := sourceByteOffset(data, token.Position.Line, 1)
	if err != nil {
		return nil, err
	}
	end := bytes.IndexByte(data[offset:], '\n')
	if end < 0 {
		end = len(data) - offset
	} else {
		end++
	}
	return append(append([]byte(nil), data[:offset]...), data[offset+end:]...), nil
}

func sourceByteOffset(data []byte, line, column int) (int, error) {
	if line < 1 || column < 1 {
		return 0, fmt.Errorf("invalid source coordinate %d:%d", line, column)
	}
	offset := 0
	for current := 1; current < line; current++ {
		next := bytes.IndexByte(data[offset:], '\n')
		if next < 0 {
			return 0, fmt.Errorf("source has no line %d", line)
		}
		offset += next + 1
	}
	remaining := data[offset:]
	for current := 1; current < column; current++ {
		if len(remaining) == 0 || remaining[0] == '\n' {
			return 0, fmt.Errorf("source line %d has no column %d", line, column)
		}
		_, size := utf8.DecodeRune(remaining)
		offset += size
		remaining = remaining[size:]
	}
	return offset, nil
}

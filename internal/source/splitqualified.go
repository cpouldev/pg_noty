package source

import "strings"

// SplitQualified parses the exact quoted spelling emitted by schema.Qualified and emits no SQL.
// It is therefore the one double-quote writer-scan exemption: the events.table_name round trip uses
// it, and so does the hostile-table-name injection proof. Dot-splitting is not an inverse when
// identifiers themselves contain dots.
func SplitQualified(stored string) (schemaName, tableName string, ok bool) {
	schemaName, next, ok := splitQualifiedPart(stored, 0)
	if !ok || next >= len(stored) || stored[next] != '.' {
		return "", "", false
	}
	tableName, next, ok = splitQualifiedPart(stored, next+1)
	if !ok || next != len(stored) || schemaName == "" || tableName == "" {
		return "", "", false
	}
	return schemaName, tableName, true
}

func splitQualifiedPart(stored string, start int) (string, int, bool) {
	if start >= len(stored) || stored[start] != '"' {
		return "", 0, false
	}
	var part strings.Builder
	for index := start + 1; index < len(stored); {
		if stored[index] != '"' {
			part.WriteByte(stored[index])
			index++
			continue
		}
		if index+1 < len(stored) && stored[index+1] == '"' {
			part.WriteByte('"')
			index += 2
			continue
		}
		return part.String(), index + 1, true
	}
	return "", 0, false
}

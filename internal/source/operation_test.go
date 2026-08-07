package source

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func operationIssues(declarations phaseOneDeclarations, table []operationAbbreviation) []string {
	issues := declarationCountIssues(declarations)
	issues = append(issues, vocabularyIssues("operation", declarations.operations, operationNames(table))...)
	return issues
}

func payloadIssues(declarations phaseOneDeclarations, modes []string) []string {
	issues := declarationCountIssues(declarations)
	return append(issues, vocabularyIssues("payload mode", declarations.modes, modes)...)
}

func declarationCountIssues(declarations phaseOneDeclarations) []string {
	if declarations.count >= 2 {
		return nil
	}
	return []string{fmt.Sprintf("examined %d Phase 1 vocabulary declarations, want at least 2", declarations.count)}
}

func vocabularyIssues(kind string, declared, provided []string) []string {
	var issues []string
	for _, name := range declared {
		if !slices.Contains(provided, name) {
			issues = append(issues, fmt.Sprintf("Phase 1 %s %q has no source entry", kind, name))
		}
	}
	for _, name := range provided {
		if !slices.Contains(declared, name) {
			issues = append(issues, fmt.Sprintf("source %s %q is not admitted by Phase 1", kind, name))
		}
	}
	return issues
}

func operationNames(table []operationAbbreviation) []string {
	names := make([]string, 0, len(table))
	for _, entry := range table {
		names = append(names, entry.kind)
	}
	return names
}

func TestOperationVocabularyReconcilesWithPhaseOneSource(t *testing.T) {
	declarations := realPhaseOneDeclarations(t)
	if issues := operationIssues(declarations, operationAbbreviations[:]); len(issues) != 0 {
		t.Fatalf("operation reconciliation failed after examining %d declarations: %s",
			declarations.count, strings.Join(issues, "; "))
	}
	t.Logf("operation reconciliation examined %d Phase 1 declarations", declarations.count)
}

func TestOperationVocabularyControlsBothDriftDirections(t *testing.T) {
	base := `var schemaLevels = map[int]struct{}{levelOperations: {keys: []keySpec{key("insert", x), key("update", x), key("delete", x)}}}; var payloadModes = []string{"full", "columns", "keys_only"}`
	for _, tc := range []struct {
		name, source string
		want         string
	}{
		{"fourth operation", strings.Replace(base, `key("delete", x)}}`, `key("delete", x), key("truncate", x)}}`, 1), "truncate"},
		{"missing operation", strings.Replace(base, `key("delete", x)}}`, `}}`, 1), "delete"},
		{"only one declaration", `var schemaLevels = map[int]struct{}{0: {key("insert", x)}}`, "want at least 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues := operationIssues(phaseOneSources(t, tc.source), operationAbbreviations[:])
			if !strings.Contains(strings.Join(issues, "; "), tc.want) {
				t.Fatalf("operationIssues reported %v, want %q", issues, tc.want)
			}
		})
	}
}

func TestOperationAbbreviationTableSizeIsPinned(t *testing.T) {
	if got := len(operationAbbreviations); got != 3 {
		t.Fatalf("operation abbreviation table has %d entries, want 3", got)
	}
}

func TestUnknownOperationIsRefusedByName(t *testing.T) {
	_, err := operationAbbreviationFor("truncate")
	if err == nil || !strings.Contains(err.Error(), "truncate") {
		t.Fatalf("operationAbbreviationFor returned %v, want a named refusal containing truncate", err)
	}
	if _, named := err.(unknownOperationError); !named {
		t.Fatalf("operationAbbreviationFor returned %T, want unknownOperationError", err)
	}
}

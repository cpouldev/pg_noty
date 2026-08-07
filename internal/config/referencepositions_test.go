package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

type operationPositionLiterals struct {
	columns string
	when    string
}

// semanticConfig removes only the private provenance fields bounded by
// TestResolvedTriggerTypesCarryOnlyTheDeclaredPrivatePositions. These coordinates
// identify source syntax for later diagnostics; they are not semantic sugar output.
func semanticConfig(cfg Config) Config {
	cfg.Listeners = slices.Clone(cfg.Listeners)
	for i := range cfg.Listeners {
		cfg.Listeners[i].Trigger.Operations = slices.Clone(cfg.Listeners[i].Trigger.Operations)
		cfg.Listeners[i].Trigger.tablePosition = nil
		for j := range cfg.Listeners[i].Trigger.Operations {
			cfg.Listeners[i].Trigger.Operations[j].columnsPosition = nil
			cfg.Listeners[i].Trigger.Operations[j].whenPosition = nil
		}
	}
	return cfg
}

func assertNormalizationOnlyClearsSourcePositions(t *testing.T, raw Config) {
	t.Helper()
	normalized := semanticConfig(raw)
	restored := normalized
	restored.Listeners = slices.Clone(restored.Listeners)
	for i := range restored.Listeners {
		restored.Listeners[i].Trigger.Operations =
			slices.Clone(restored.Listeners[i].Trigger.Operations)
		restored.Listeners[i].Trigger.tablePosition = raw.Listeners[i].Trigger.tablePosition
		for j := range restored.Listeners[i].Trigger.Operations {
			restored.Listeners[i].Trigger.Operations[j].columnsPosition =
				raw.Listeners[i].Trigger.Operations[j].columnsPosition
			restored.Listeners[i].Trigger.Operations[j].whenPosition =
				raw.Listeners[i].Trigger.Operations[j].whenPosition
		}
	}
	if !reflect.DeepEqual(restored, raw) {
		t.Fatal("normalization changed a field outside tablePosition, columnsPosition or whenPosition")
	}
}

func assertOnlySourcePositionsDiffer(t *testing.T, left, right Config) {
	t.Helper()
	if reflect.DeepEqual(left, right) {
		t.Fatal("raw configs are equal; syntax-specific provenance was not exercised")
	}
	if !reflect.DeepEqual(semanticConfig(left), semanticConfig(right)) {
		t.Fatal("map/list forms differ outside the three known source-position fields")
	}
}

func assertReferenceSourcePositions(t *testing.T, data []byte, file string, cfg Config,
	want map[string]operationPositionLiterals,
) {
	t.Helper()
	trigger := cfg.Listeners[0].Trigger
	assertLiteralPosition(t, data, file, "public.orders", "listeners[0].table",
		trigger.tablePosition)
	if len(trigger.Operations) != len(want) {
		t.Fatalf("%s has %d operations, want %d position specifications",
			file, len(trigger.Operations), len(want))
	}
	for _, operation := range trigger.Operations {
		literals, exists := want[operation.Kind]
		if !exists {
			t.Errorf("%s operation %q has no position specification", file, operation.Kind)
			continue
		}
		prefix := "listeners[0].operations." + operation.Kind
		assertLiteralPosition(t, data, file, literals.columns, prefix+".columns",
			operation.columnsPosition)
		assertLiteralPosition(t, data, file, literals.when, prefix+".when",
			operation.whenPosition)
	}
}

func assertLiteralPosition(t *testing.T, data []byte, file, literal, path string,
	position Positioned,
) {
	t.Helper()
	if literal == "" {
		if position == nil || position.File() != file || position.Line() != 0 ||
			position.Col() != 0 || position.Path() != "" {
			t.Errorf("absent %s position = %v, want file-only provenance for %s",
				path, position, file)
		}
		return
	}
	text := string(data)
	at := strings.Index(text, literal)
	if at < 0 {
		t.Fatalf("%s lacks literal %q required by its position oracle", file, literal)
	}
	line := strings.Count(text[:at], "\n") + 1
	lineStart := strings.LastIndex(text[:at], "\n") + 1
	column := utf8.RuneCountInString(text[lineStart:at]) + 1
	if position == nil || position.File() != file || position.Path() != path ||
		position.Line() != line || position.Col() != column {
		t.Errorf("%q position = %v, want %s:%d:%d %s",
			literal, position, file, line, column, path)
	}
}

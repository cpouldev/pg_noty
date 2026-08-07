package config

import (
	"reflect"
	"slices"
	"testing"
)

// wrappersBeneathTheRawTree derives the count from the raw structs, so a new field moves
// both the fixture obligation and the walk's expected work.
func wrappersBeneathTheRawTree() int {
	return countWrappersIn(reflect.TypeFor[rawConfig]())
}

func countWrappersIn(held reflect.Type) int {
	if held.Kind() == reflect.Slice {
		return countWrappersIn(held.Elem())
	}
	if held.Kind() != reflect.Struct {
		return 0
	}
	counted := 0
	for i := range held.NumField() {
		field := held.Field(i)
		if !field.IsExported() {
			continue
		}
		if implementsReadValue(field.Type) {
			counted++
		}
		counted += countWrappersIn(field.Type)
	}
	return counted
}

func implementsReadValue(held reflect.Type) bool {
	return reflect.PointerTo(held).Implements(reflect.TypeFor[readValue]())
}

// readValue deliberately filters on wasWritten rather than Valid: unreadable wrappers
// are exactly the ones whose recorded refusal still needs a resolved position.
type readValue interface {
	Positioned
	Valid() bool
	wasWritten() bool
}

func unresolvedWrappersIn(t *testing.T, decoded *rawConfig) []string {
	t.Helper()
	var unresolved []string
	eachReadValueIn(reflect.ValueOf(decoded), "", func(path string, held readValue) {
		if held.Line() == 0 {
			unresolved = append(unresolved, path)
		}
	})
	slices.Sort(unresolved)
	return unresolved
}

func resolvedWrapperCount(t *testing.T, decoded *rawConfig) int {
	t.Helper()
	reached := 0
	eachReadValueIn(reflect.ValueOf(decoded), "", func(string, readValue) { reached++ })
	return reached
}

// eachReadValueIn is an independent reflection walk; reusing production's walk would
// make the oracle unable to see a subject production omitted.
func eachReadValueIn(held reflect.Value, path string, visit func(string, readValue)) {
	if held.Kind() == reflect.Pointer {
		if held.IsNil() {
			return
		}
		held = held.Elem()
	}
	if !held.CanInterface() {
		return
	}
	if read, isValue := held.Interface().(readValue); isValue && read.wasWritten() {
		visit(path, read)
	}
	switch held.Kind() {
	case reflect.Struct:
		for i := range held.NumField() {
			field := held.Type().Field(i)
			if field.IsExported() {
				eachReadValueIn(held.Field(i), path+"."+field.Name, visit)
			}
		}
	case reflect.Slice:
		for i := range held.Len() {
			eachReadValueIn(held.Index(i), path+"[]", visit)
		}
	}
}

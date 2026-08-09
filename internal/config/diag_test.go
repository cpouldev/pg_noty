package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestEmptyDiagnosticCollectionsNormalizeToNil pins what a successful load returns:
// nothing at all, so a caller can test for absence with a nil check.
func TestEmptyDiagnosticCollectionsNormalizeToNil(t *testing.T) {
	if got := (Errors{}).normalized(); got != nil {
		t.Errorf("Errors{}.normalized() = %+v, want nil", got)
	}
	if got := (Warnings{}).normalized(); got != nil {
		t.Errorf("Warnings{}.normalized() = %+v, want nil", got)
	}
}

func TestWarningsNormalizeExactlyLikeErrors(t *testing.T) {
	input := Warnings{
		{Rule: W2, File: "a.yaml", Line: 9, Col: 3, Path: "worker.drain_timeout", Msg: "shorter than timeout"},
		{Rule: W1, File: "a.yaml", Line: 2, Col: 5, Path: "listeners[0].destination.signing.secrets[0]", Msg: "literal secret"},
		{Rule: W1, File: "a.yaml", Line: 2, Col: 5, Path: "listeners[0].destination.signing.secrets[0]", Msg: "literal secret"},
	}

	got := input.normalized()

	if len(got) != 2 {
		t.Fatalf("normalized() returned %d warnings, want 2", len(got))
	}
	if got[0].Line != 2 || got[1].Line != 9 {
		t.Errorf("normalized() ordered lines %d then %d, want 2 then 9", got[0].Line, got[1].Line)
	}
}

func TestErrorConstructorRequiresARuleID(t *testing.T) {
	// A diagnostic cannot exist without naming the rule that produced it:
	//
	//	NewError(sourcePos{line: 1}, "boom")  // does not compile:
	//	                                      // RuleID argument missing
	//
	// The reflection below pins that signature so a future refactor that makes
	// Rule optional fails here rather than silently voiding rule traceability.
	tests := []struct {
		name string
		ctor any
	}{
		{name: "NewError", ctor: NewError},
		{name: "NewFileError", ctor: NewFileError},
		{name: "NewWarning", ctor: NewWarning},
	}

	wantFirst := reflect.TypeOf(RuleID(""))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			signature := reflect.TypeOf(tc.ctor)
			if got := signature.In(0); got != wantFirst {
				t.Errorf("first parameter is %s, want %s", got, wantFirst)
			}
		})
	}
}

func TestConstructorFillsEveryDiagnosticField(t *testing.T) {
	// A position carries the raw line and the parser's column rather than a column, so
	// the expected Col below is derived: `      colums: [id]` is six spaces, putting
	// the key at rune 7, which is also what the parser reports when no tab precedes it.
	where := sourcePos{
		file:           "listeners.yaml",
		line:           14,
		lineText:       "      colums: [id]",
		reportedColumn: 7,
		path:           "listeners[0].payload.colums",
	}

	diag := NewError(R41, where, `unknown field "colums"`).WithHint(`did you mean "columns"?`)

	want := Error{
		Rule: R41,
		File: "listeners.yaml",
		Line: 14,
		Col:  7,
		Path: "listeners[0].payload.colums",
		Msg:  `unknown field "colums"`,
		Hint: `did you mean "columns"?`,
	}
	if diag != want {
		t.Errorf("NewError(...) = %+v, want %+v", diag, want)
	}
}

// TestWarningCarriesTheSameFieldsAsAnError exercises the warning constructors rather
// than only inspecting their types, because W1 and W2 reach a caller through these
// two bodies and nothing else.
func TestWarningCarriesTheSameFieldsAsAnError(t *testing.T) {
	// `        - hunter2` is eight spaces, so the list element begins at rune 9.
	where := sourcePos{
		file:           "listeners.yaml",
		line:           22,
		lineText:       "        - hunter2",
		reportedColumn: 9,
		path:           "listeners[0].destination.signing.secrets[0]",
	}

	got := NewWarning(W1, where, "signing secret is a literal").WithHint("reference an environment variable instead")

	want := Warning{
		Rule: W1,
		File: "listeners.yaml",
		Line: 22,
		Col:  9,
		Path: "listeners[0].destination.signing.secrets[0]",
		Msg:  "signing secret is a literal",
		Hint: "reference an environment variable instead",
	}
	if got != want {
		t.Errorf("NewWarning(...).WithHint(...) = %+v, want %+v", got, want)
	}
}

func TestFileErrorCarriesNoPosition(t *testing.T) {
	diag := NewFileError(RuleRead, "missing.yaml", "cannot read config file: no such file or directory")

	if diag.Line != 0 || diag.Col != 0 || diag.Path != yamlPathRoot {
		t.Errorf("NewFileError(...) = %+v, want zero Line and Col and root Path", diag)
	}
	if diag.File != "missing.yaml" {
		t.Errorf("NewFileError(...).File = %q, want %q", diag.File, "missing.yaml")
	}
}

func TestPositionedNamesNoLibraryType(t *testing.T) {
	positioned := reflect.TypeOf((*Positioned)(nil)).Elem()

	for i := range positioned.NumMethod() {
		method := positioned.Method(i)
		signature := method.Type
		for in := range signature.NumIn() {
			assertNotALibraryType(t, method.Name, signature.In(in))
		}
		for out := range signature.NumOut() {
			assertNotALibraryType(t, method.Name, signature.Out(out))
		}
	}
}

func assertNotALibraryType(t *testing.T, method string, typ reflect.Type) {
	t.Helper()
	if strings.Contains(typ.PkgPath(), "goccy") {
		t.Errorf("Positioned.%s names library type %s.%s; L2 rule code must read positions without importing the parser",
			method, typ.PkgPath(), typ.Name())
	}
}

package config

import (
	"slices"
	"testing"
)

func TestAnOperationNameCarriesThePositionOfTheKeyItWasWrittenAs(t *testing.T) {
	decoded := oneDecodedListener(t, operationsOf("    operations:\n      insert: {}\n"+
		"      update: {}\n      delete: {}\n"))
	if len(decoded.Operations.values) != 3 {
		t.Fatalf("decoded %d operations, want 3", len(decoded.Operations.values))
	}
	for i, named := range decoded.Operations.values {
		if wantLine := 8 + i; named.Name.Line() != wantLine || named.Name.Col() != 7 {
			t.Errorf("%q is at %d:%d, want %d:7",
				named.Name.value, named.Name.Line(), named.Name.Col(), wantLine)
		}
	}
}

func TestAFoldedListEntryCarriesThePositionTheAuthorWrote(t *testing.T) {
	decoded := oneDecodedListener(t, operationsOf("    operations: [insert, update]\n"))
	wantColumns := map[string]int{"insert": 18, "update": 26}
	if len(decoded.Operations.values) != len(wantColumns) {
		t.Fatalf("decoded %d operations, want 2", len(decoded.Operations.values))
	}
	for _, named := range decoded.Operations.values {
		want, expected := wantColumns[named.Name.value]
		if !expected {
			t.Errorf("decoded unwritten operation %q", named.Name.value)
			continue
		}
		if named.Name.Line() != 7 || named.Name.Col() != want {
			t.Errorf("%q is at %d:%d, want 7:%d",
				named.Name.value, named.Name.Line(), named.Name.Col(), want)
		}
	}
}

func TestAnOperationsFiltersAreDecodedAndPositioned(t *testing.T) {
	decoded := oneDecodedListener(t, operationsOf("    operations:\n      update:\n"+
		"        columns: [status, total]\n        is_distinct: true\n        when: NEW.total > 0\n"))
	if len(decoded.Operations.values) != 1 {
		t.Fatalf("decoded %d operations, want 1", len(decoded.Operations.values))
	}
	update := decoded.Operations.values[0].Filter
	if got := textsIn(update.Columns); !slices.Equal(got, []string{"status", "total"}) {
		t.Errorf("columns = %v, want the two written", got)
	}
	if len(update.Columns.values) == 2 {
		for i, want := range []int{19, 27} {
			if element := update.Columns.values[i]; element.Line() != 9 || element.Col() != want {
				t.Errorf("column %d is at %d:%d, want 9:%d",
					i, element.Line(), element.Col(), want)
			}
		}
	}
	if !update.IsDistinct.value || update.IsDistinct.Line() != 10 || update.IsDistinct.Col() != 22 {
		t.Errorf("is_distinct is %t at %d:%d, want true at 10:22",
			update.IsDistinct.value, update.IsDistinct.Line(), update.IsDistinct.Col())
	}
	if update.When.value != "NEW.total > 0" {
		t.Errorf("when = %q, want the written condition", update.When.value)
	}
	if update.When.Line() != 11 || update.When.Col() != 15 {
		t.Errorf("when is at %d:%d, want 11:15", update.When.Line(), update.When.Col())
	}
}

func TestTheOperationsDecodeRefusesAShapeItCannotReadRatherThanFailing(t *testing.T) {
	tests := []struct {
		name, document, path string
		want                 fault
	}{
		{"a value that is not a mapping", "operations: [insert]\n", "$.operations", mustBeAnOperationsMapping},
		{"a key that carries no name", "operations:\n  16: {}\n", "$.operations", mustBeAnOperationName},
		{"a filter that is not a mapping", "operations:\n  insert: text\n", "$.operations", mustBeAnOperationsMapping},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, node := nodeAt(t, tc.document, tc.path)
			var held rawOperations
			if err := held.UnmarshalYAML(node); err != nil {
				t.Fatalf("UnmarshalYAML returned %v", err)
			}
			pass := newDecodePass(src)
			held.resolve(pass)
			if len(pass.diags) != 1 {
				t.Fatalf("recorded %q, want one refusal", messagesOf(pass.diags))
			}
			if pass.diags[0].Msg != tc.want.message {
				t.Errorf("Msg = %q, want %q", pass.diags[0].Msg, tc.want.message)
			}
			if !held.Set || held.Valid() {
				t.Errorf("Set = %t, Valid = %t; want written and unread", held.Set, held.Valid())
			}
		})
	}
}

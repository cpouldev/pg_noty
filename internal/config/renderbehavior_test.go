package config

import (
	"strings"
	"testing"
)

func TestRenderedOutputCarriesNoColour(t *testing.T) {
	diags := Errors{
		unknownPayloadKey(),
		NewFileError(RuleRead, "listeners.yaml",
			"cannot read configuration file: no such file or directory"),
	}
	rendered := diags.Render([]byte(authoritativeSource)) +
		Warnings{Warning(unknownPayloadKey())}.Render([]byte(authoritativeSource))
	if strings.ContainsRune(rendered, 0x1b) {
		t.Errorf("rendered output contains an escape byte:\n%q", rendered)
	}
}

func TestWarningsShareTheLayoutAndAreMarked(t *testing.T) {
	diag := unknownPayloadKey()
	asError := Errors{diag}.Render([]byte(authoritativeSource))
	asWarning := Warnings{Warning(diag)}.Render([]byte(authoritativeSource))
	if asWarning == asError {
		t.Fatal("a warning renders exactly like an error, so nothing marks it as a warning")
	}
	if !strings.Contains(asWarning, "warning: ") {
		t.Errorf("warning is not marked as one:\n%s", asWarning)
	}
	if stripped := strings.Replace(asWarning, "warning: ", "", 1); stripped != asError {
		t.Errorf("warning layout differs from the error layout once the mark is removed:\n%s\nwant:\n%s\n%s",
			stripped, asError, goldenDiff(asError, stripped))
	}
}

func TestADiagnosticWithoutAPositionRendersWithoutASnippet(t *testing.T) {
	diag := NewFileError(RuleRead, "listeners.yaml",
		"cannot read configuration file: no such file or directory").WithHint("check the path")
	got := Errors{diag}.Render(nil)
	want := "listeners.yaml\n" +
		"cannot read configuration file: no such file or directory\n" +
		"  check the path\n"
	if got != want {
		t.Errorf("rendered output:\n%s\nwant:\n%s\n%s", got, want, goldenDiff(want, got))
	}
}

func TestEachDiagnosticRendersItsOwnBlock(t *testing.T) {
	first := Error{File: "listeners.yaml", Line: 1, Col: 1, Msg: "first"}
	second := Error{File: "listeners.yaml", Line: 2, Col: 3, Msg: "second"}
	got := Errors{first, second}.Render([]byte("one\ntwo\n"))
	want := `listeners.yaml:1:1
>  1 | one
       ^ first

listeners.yaml:2:3
   1 | one
>  2 | two
         ^ second
`
	if got != want {
		t.Errorf("rendered output:\n%s\nwant:\n%s\n%s", got, want, goldenDiff(want, got))
	}
}

func TestRenderingNothingProducesNothing(t *testing.T) {
	if got := Errors(nil).Render([]byte(authoritativeSource)); got != "" {
		t.Errorf("Errors(nil).Render() = %q, want the empty string", got)
	}
	if got := Warnings(nil).Render([]byte(authoritativeSource)); got != "" {
		t.Errorf("Warnings(nil).Render() = %q, want the empty string", got)
	}
}

func TestBothSnippetMarkersAreTheSameWidth(t *testing.T) {
	if len(offendingMarker) != len(contextMarker) {
		t.Errorf("offendingMarker is %d wide and contextMarker %d; the caret would drift between them",
			len(offendingMarker), len(contextMarker))
	}
}

func TestATrailingSpaceIsNeverAdded(t *testing.T) {
	tests := []struct {
		name string
		diag Error
	}{
		{name: "a quoted empty line",
			diag: Error{File: "blank.yaml", Line: 3, Col: 1, Msg: "message"}},
		{name: "a caret with no message beside it",
			diag: Error{File: "blank.yaml", Line: 3, Col: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Errors{tc.diag}.Render([]byte("one\n\nthree\n"))
			if got == "" {
				t.Fatal("nothing was rendered, so this would pass vacuously")
			}
			for _, line := range strings.Split(got, "\n") {
				if line != strings.TrimRight(line, " ") {
					t.Errorf("line %q ends in whitespace\nfull output:\n%s", line, got)
				}
			}
		})
	}
}

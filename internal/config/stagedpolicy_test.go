package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// Stage D's error policy: accumulate across the whole document, then stop (load.go).
//
// The three claims here are what "accumulate then stop" is made of -- every occurrence is
// reported rather than the first, nothing a later stage would find is reported alongside them,
// and a stop at this stage carries no warnings.

// TestStageDAccumulatesAcrossTheWholeDocumentThenStops is AC #29 and AC #23 case 3 from
// both sides, on one fixture run twice.
//
// With the variables unset the run reports both references and none of the four later mistakes the
// fixture also carries -- an unknown key, a value of the wrong type, an out-of-enum method and a
// destination scheme that is not HTTP -- which is what "stops before decode" means. With them set the
// run passes stage D and reports every one a wired stage can see.
//
// **Two corrections to what stood here, both made by Step 6.** The fixture's method was `PATCH`, which
// R37 permits, so the "out-of-enum method" its readers counted was not a mistake at all; it is `DELETE`
// now. And the fixture gained `worker.concurrency: abc`, appended after every line the golden's blocks
// quote so that no line number inside them moves -- a mistake stage G can see, so that "the run does not
// stop at the first later mistake" is asserted across two stages rather than claimed of one.
func TestStageDAccumulatesAcrossTheWholeDocumentThenStops(t *testing.T) {
	path := filepath.Join(invalidCorpus, "unresolved_before_later_mistakes.yaml")
	data, name := readFixtureBytes(t, path), filepath.Base(path)

	t.Run("variables unset", func(t *testing.T) {
		cfg, warnings, errs := Parse(data, name, MapEnv(nil))

		if len(errs) != 2 {
			t.Fatalf("got %d diagnostics %q, want the two unresolved references", len(errs), messagesOf(errs))
		}
		for _, diag := range errs {
			if diag.Rule != RuleInterpolate {
				t.Errorf("Rule = %q, want every diagnostic of this run to come from stage D", diag.Rule)
			}
		}
		if len(warnings) != 0 {
			t.Errorf("got %d warnings, want none: W1 and W2 are computed in stages H and I", len(warnings))
		}
		if cfg != nil {
			t.Error("a run that stopped at stage D returned a configuration")
		}
	})

	// With both references resolved the run reaches every wired accumulating stage: stage F finds the
	// unknown key, stage G the non-integer and stage H the method.
	//
	// Step 9 adds the remaining non-HTTP scheme (R36), making the final count
	// four.
	t.Run("variables set", func(t *testing.T) {
		_, _, errs := Parse(data, name, MapEnv(map[string]string{
			"DATABASE_URL":     "postgres://noty@db.internal/noty",
			"LISTENER_TIMEOUT": "10s",
		}))

		found := make(map[RuleID]int, 4)
		for _, diag := range errs {
			found[diag.Rule]++
		}

		if len(errs) != 4 {
			t.Fatalf("got %q, want the four later mistakes wired stages can see", messagesOf(errs))
		}
		if found[R41] != 1 {
			t.Errorf("got %d diagnostics under R41, want the unknown key stage F owns", found[R41])
		}
		if found[RuleDecode] != 1 {
			t.Errorf("got %d diagnostics under %q, want the value stage G could not read",
				found[RuleDecode], RuleDecode)
		}
		if found[R37] != 1 {
			t.Errorf("got %d diagnostics under R37, want the method stage H refuses", found[R37])
		}
		if found[R36] != 1 {
			t.Errorf("got %d diagnostics under R36, want the destination scheme stage H refuses", found[R36])
		}
	})
}

// TestEveryUnresolvedOccurrenceIsReportedWithNoFieldLeftEmpty is AC #7: three distinct
// unset variables written across three lines, one of them twice, are four diagnostics --
// one per occurrence -- ordered by position and each naming its own variable.
//
// The fixture's derivation: DATABASE_URL is written on lines 3 and 5, DATABASE_SCHEMA on
// line 4 and INSTANCE_SUFFIX on line 5. The two occurrences sharing line 5 share a column
// as well, because both are inside one scalar whose node is what a diagnostic anchors on;
// they survive de-duplication because they name different variables, which is exactly the
// per-occurrence claim.
func TestEveryUnresolvedOccurrenceIsReportedWithNoFieldLeftEmpty(t *testing.T) {
	path := filepath.Join(invalidCorpus, "unresolved_references.yaml")

	_, _, errs := Parse(readFixtureBytes(t, path), filepath.Base(path), MapEnv(nil))

	want := []struct {
		line     int
		column   int
		variable string
	}{
		// `  url: ` is seven runes, so the value begins at rune 8.
		{line: 3, column: 8, variable: "DATABASE_URL"},
		// `  schema: ` is ten runes, so the value begins at rune 11.
		{line: 4, column: 11, variable: "DATABASE_SCHEMA"},
		// `instance: ` is ten runes; both of that line's references anchor on its value.
		{line: 5, column: 11, variable: "DATABASE_URL"},
		{line: 5, column: 11, variable: "INSTANCE_SUFFIX"},
	}

	if len(errs) != len(want) {
		t.Fatalf("Parse() returned %d diagnostics, want %d: %q", len(errs), len(want), messagesOf(errs))
	}
	for i, expected := range want {
		diag := errs[i]

		if diag.Line != expected.line || diag.Col != expected.column {
			t.Errorf("diagnostic %d is at %d:%d, want %d:%d", i, diag.Line, diag.Col, expected.line, expected.column)
		}
		if !strings.Contains(diag.Msg, expected.variable) {
			t.Errorf("diagnostic %d is %q, want it to name %s", i, diag.Msg, expected.variable)
		}
		assertNoFieldIsEmpty(t, diag)
	}
}

// assertNoFieldIsEmpty is AC #7's "no field left empty": a stage-D diagnostic states its
// rule, its file, where it is, what it is about, what is wrong and how to fix it.
func assertNoFieldIsEmpty(t *testing.T, diag Error) {
	t.Helper()

	named := []struct {
		field string
		empty bool
	}{
		{field: "Rule", empty: diag.Rule == ""},
		{field: "File", empty: diag.File == ""},
		{field: "Line", empty: diag.Line < firstLine},
		{field: "Col", empty: diag.Col < firstColumn},
		{field: "Path", empty: diag.Path == ""},
		{field: "Msg", empty: diag.Msg == ""},
		{field: "Hint", empty: diag.Hint == ""},
	}
	for _, field := range named {
		if field.empty {
			t.Errorf("%s is empty on %+v", field.field, diag)
		}
	}
}

// TestOccurrencesOfOneVariableInOneScalarCollapseToOneDiagnostic pins Implementation Note 7,
// which is otherwise prose.
//
// Per-occurrence reporting is per occurrence *per message*. A stage-D diagnostic anchors on
// the value node, so two references written inside one scalar share a file, a line and a
// column, and de-duplication keys on those together with the message: two occurrences of the
// same variable collapse, two of different variables do not. That is the whole reason AC #7's
// fixture writes the repeated variable's second occurrence beside a different one, and it is
// what makes four occurrences four diagnostics rather than three.
//
// Both halves are asserted from one shape of input, so a change to the de-duplication key
// cannot alter AC #7's count while leaving this green.
func TestOccurrencesOfOneVariableInOneScalarCollapseToOneDiagnostic(t *testing.T) {
	tests := []struct {
		name      string
		written   string
		variables []string
	}{
		{name: "the same variable written twice", written: "${A}-${A}", variables: []string{"A"}},
		{name: "two different variables", written: "${A}-${B}", variables: []string{"A", "B"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, errs := Parse([]byte("value: "+tc.written+"\n"), interpolationFixture, MapEnv(nil))

			if len(errs) != len(tc.variables) {
				t.Fatalf("got %d diagnostics %q, want one per variable named: %q",
					len(errs), messagesOf(errs), tc.variables)
			}
			for i, variable := range tc.variables {
				if !strings.Contains(errs[i].Msg, fmt.Sprintf("%q", variable)) {
					t.Errorf("diagnostic %d is %q, want it to name %s", i, errs[i].Msg, variable)
				}
				// `value: ` is seven runes, so both occurrences anchor at rune 8 of line 1.
				// Sharing a position is what the collapse rests on: were the two ever
				// positioned apart, the same-variable row would report twice.
				if errs[i].Line != 1 || errs[i].Col != 8 {
					t.Errorf("diagnostic %d is at %d:%d, want the value's own position 1:8",
						i, errs[i].Line, errs[i].Col)
				}
			}
		})
	}
}

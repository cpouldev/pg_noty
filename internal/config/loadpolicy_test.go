package config

import (
	"path/filepath"
	"slices"
	"testing"
)

// TestStructuralConditionsAreDistributedTwoOneThreeAcrossStages pins stage ownership.
func TestStructuralConditionsAreDistributedTwoOneThreeAcrossStages(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.yaml")
	unreadable := t.TempDir()
	viaLoad := func(path string) func() Errors {
		return func() Errors {
			_, _, errs := Load(path, MapEnv(nil))
			return errs
		}
	}
	viaParse := func(src string) func() Errors {
		return func() Errors {
			_, _, errs := Parse([]byte(src), "listeners.yaml", MapEnv(nil))
			return errs
		}
	}
	conditions := []struct {
		name  string
		stage RuleID
		run   func() Errors
	}{
		{"nonexistent path", RuleRead, viaLoad(absent)},
		{"unreadable path", RuleRead, viaLoad(unreadable)},
		{"duplicate mapping key", R42, viaParse("a: 1\na: 2\n")},
		{"empty or comment-only file", RuleDocument, viaParse("# only a comment\n")},
		{"sequence root", RuleDocument, viaParse("- one\n")},
		{"two documents", RuleDocument, viaParse("a: 1\n---\nb: 2\n")},
	}
	perStage := make(map[RuleID]int, 3)
	for _, condition := range conditions {
		t.Run(condition.name, func(t *testing.T) {
			errs := condition.run()
			if len(errs) != 1 {
				t.Fatalf("returned %d diagnostics, want exactly 1: %+v", len(errs), errs)
			}
			if errs[0].Rule != condition.stage {
				t.Errorf("Rule = %q, want %q", errs[0].Rule, condition.stage)
			}
			perStage[errs[0].Rule]++
		})
	}
	for stage, count := range map[RuleID]int{RuleRead: 2, R42: 1, RuleDocument: 3} {
		if perStage[stage] != count {
			t.Errorf("stage %q owns %d conditions, want %d", stage, perStage[stage], count)
		}
	}
}

func TestParseReturnsNoConfigWheneverThereAreDiagnostics(t *testing.T) {
	tests := []struct{ name, src string }{
		{"empty document", ""},
		{"sequence root", "- one\n"},
		{"two documents", "a: 1\n---\nb: 2\n"},
		{"duplicate key", "a: 1\na: 2\n"},
		{"syntax error", "a: [1\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _, errs := Parse([]byte(tc.src), "listeners.yaml", MapEnv(nil))
			if len(errs) == 0 {
				t.Fatal("Parse() reported no diagnostics for an invalid document")
			}
			if cfg != nil {
				t.Error("Parse() returned a configuration alongside diagnostics")
			}
		})
	}
}

// TestParseReturnsAConfigOnceStageIResolvesAValidDocument pins the completed stage-I boundary.
func TestParseReturnsAConfigOnceStageIResolvesAValidDocument(t *testing.T) {
	cfg, warnings, errs := Parse([]byte("version: 1\ndatabase:\n  url: &shared ${DATABASE_URL}\n"+
		"instance: noty\nlisteners: []\n"),
		"listeners.yaml", MapEnv(map[string]string{
			"DATABASE_URL": "postgres://noty@db.internal/noty",
		}))
	if len(errs) != 0 {
		t.Errorf("Parse() returned diagnostics for a valid document: %+v", errs)
	}
	if len(warnings) != 0 {
		t.Errorf("Parse() returned %d warnings before warning stages", len(warnings))
	}
	if cfg == nil {
		t.Error("Parse() returned no configuration after stage I")
	}
}

func TestTheBoundaryNormalizesBothCollections(t *testing.T) {
	later := Error{Rule: R17, File: "listeners.yaml", Line: 9, Col: 1, Msg: "reported second"}
	earlier := Error{Rule: R17, File: "listeners.yaml", Line: 2, Col: 1, Msg: "reported first"}
	cfg, warnings, errs := normalizedResult(nil,
		Warnings{Warning(later), Warning(earlier), Warning(later)},
		Errors{later, earlier, later},
	)
	if cfg != nil {
		t.Error("normalizedResult() invented a configuration")
	}
	if len(errs) != 2 || errs[0].Line != 2 {
		t.Errorf("errors = %+v, want the duplicate collapsed and line 2 first", errs)
	}
	if len(warnings) != 2 || warnings[0].Line != 2 {
		t.Errorf("warnings = %+v, want the duplicate collapsed and line 2 first", warnings)
	}
	if !slices.Equal([]int{errs[0].Line, warnings[0].Line}, []int{2, 2}) {
		t.Error("both collections must use the same ordering")
	}
}

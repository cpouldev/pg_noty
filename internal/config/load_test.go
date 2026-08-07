package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestEntryPointSignaturesMatchTheContract pins the two entry points that all
// five later packages bind to by name. A rename or a reordered parameter must fail
// here rather than in a downstream package.
func TestEntryPointSignaturesMatchTheContract(t *testing.T) {
	tests := []struct {
		name    string
		fn      any
		wantIn  []string
		wantOut []string
	}{
		{
			name:    "Load",
			fn:      Load,
			wantIn:  []string{"string", "config.EnvLookup"},
			wantOut: []string{"*config.Config", "config.Warnings", "config.Errors"},
		},
		{
			name:    "Parse",
			fn:      Parse,
			wantIn:  []string{"[]uint8", "string", "config.EnvLookup"},
			wantOut: []string{"*config.Config", "config.Warnings", "config.Errors"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			signature := reflect.TypeOf(tc.fn)

			if got := typeNames(signature, signature.NumIn, signature.In); !equalStrings(got, tc.wantIn) {
				t.Errorf("parameters = %v, want %v", got, tc.wantIn)
			}
			if got := typeNames(signature, signature.NumOut, signature.Out); !equalStrings(got, tc.wantOut) {
				t.Errorf("results = %v, want %v", got, tc.wantOut)
			}
		})
	}
}

func typeNames(signature reflect.Type, count func() int, at func(int) reflect.Type) []string {
	names := make([]string, count())
	for i := range names {
		names[i] = at(i).String()
	}
	return names
}

func equalStrings(got, want []string) bool {
	return strings.Join(got, ",") == strings.Join(want, ",")
}

// TestLoadReportsOnePositionlessDiagnosticForAnUnusableFile covers stage A's two
// conditions in detail. The unreadable condition carries two fixtures rather than one: a
// directory is unreadable to every user, so it always asserts the message and position
// shape, while permission denial is the more realistic cause and can only be provoked by
// an unprivileged runner. Without the directory row the whole condition's detail
// assertions would go unasserted under a root CI runner.
func TestLoadReportsOnePositionlessDiagnosticForAnUnusableFile(t *testing.T) {
	dir := t.TempDir()
	unreadable := filepath.Join(dir, "unreadable.yaml")
	if err := os.WriteFile(unreadable, []byte("version: 1\n"), 0o000); err != nil {
		t.Fatalf("creating the unreadable fixture failed: %v", err)
	}

	tests := []struct {
		name string
		path string
		skip string
	}{
		{
			name: "nonexistent path",
			path: filepath.Join(dir, "absent.yaml"),
		},
		{
			name: "unreadable path that is a directory",
			path: t.TempDir(),
		},
		{
			name: "unreadable path denied by its permissions",
			path: unreadable,
			skip: "a privileged user can read a 0000-mode file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip != "" && os.Geteuid() == 0 {
				t.Skip(tc.skip)
			}

			cfg, warnings, errs := Load(tc.path, MapEnv(nil))

			if len(errs) != 1 {
				t.Fatalf("Load() returned %d diagnostics, want exactly 1: %+v", len(errs), errs)
			}
			if cfg != nil {
				t.Error("Load() returned a configuration for a file it could not read")
			}
			if len(warnings) != 0 {
				t.Errorf("Load() returned %d warnings, want none before the pipeline runs", len(warnings))
			}
			if errs[0].Rule != RuleRead {
				t.Errorf("Rule = %q, want %q", errs[0].Rule, RuleRead)
			}
			if errs[0].Line != 0 || errs[0].Col != 0 {
				t.Errorf("diagnostic carries position %d:%d, want a positionless diagnostic", errs[0].Line, errs[0].Col)
			}
			if errs[0].File != tc.path {
				t.Errorf("File = %q, want %q", errs[0].File, tc.path)
			}
		})
	}
}

// TestLoadReadsStructuralFixturesFromTestdata runs the same conditions through the
// filesystem. Each wantCol is counted from the named line of the fixture on disk, so a
// derivation that changed would fail here as well as in parse_test.go.
func TestLoadReadsStructuralFixturesFromTestdata(t *testing.T) {
	tests := []struct {
		name     string
		fixture  string
		wantRule RuleID
		wantLine int
		wantCol  int
	}{
		// A file that configures nothing has no token to anchor on.
		{name: "empty file", fixture: "empty.yaml", wantRule: RuleDocument, wantLine: 0, wantCol: 0},
		{name: "comment only file", fixture: "comment_only.yaml", wantRule: RuleDocument, wantLine: 0, wantCol: 0},
		// Line 1 is `- name: order_paid`: the sequence's own token is the `-` at rune 1.
		{name: "sequence root", fixture: "sequence_root.yaml", wantRule: RuleDocument, wantLine: 1, wantCol: 1},
		// Line 4 is `version: 1`: the second document's first key begins at rune 1.
		{name: "two documents", fixture: "two_documents.yaml", wantRule: RuleDocument, wantLine: 4, wantCol: 1},
		// Line 3 is `  schema: other`: two spaces, so the repeated key is at rune 3.
		{name: "duplicate key", fixture: "duplicate_key.yaml", wantRule: R42, wantLine: 3, wantCol: 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(invalidCorpus, tc.fixture)

			cfg, _, errs := Load(path, MapEnv(nil))

			if len(errs) != 1 {
				t.Fatalf("Load(%s) returned %d diagnostics, want exactly 1: %+v", tc.fixture, len(errs), errs)
			}
			if cfg != nil {
				t.Error("Load() returned a configuration alongside a diagnostic")
			}
			if errs[0].Rule != tc.wantRule {
				t.Errorf("Rule = %q, want %q", errs[0].Rule, tc.wantRule)
			}
			if errs[0].Line != tc.wantLine {
				t.Errorf("Line = %d, want %d", errs[0].Line, tc.wantLine)
			}
			if errs[0].Col != tc.wantCol {
				t.Errorf("Col = %d, want %d", errs[0].Col, tc.wantCol)
			}
			if errs[0].File != path {
				t.Errorf("File = %q, want %q", errs[0].File, path)
			}
		})
	}
}

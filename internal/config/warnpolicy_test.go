package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestW2DistinguishesBelowEqualAndAbove(t *testing.T) {
	base := string(readFixtureBytes(t, warningFixture("W2_drain_below_timeout")))
	larger := replaceOnce(t, base, "timeout: 10s", "timeout: 8s") +
		"  - name: larger\n    table: public.larger\n    operations: [insert]\n" +
		"    timeout: 10s\n    destination:\n      url: https://hooks.example.test/larger\n"
	defaulted := strings.Replace(base, "  drain_timeout: 9s\n", "", 1)
	defaulted = replaceOnce(t, defaulted, "lease_timeout: 20s", "lease_timeout: 40s")
	defaulted = replaceOnce(t, defaulted, "timeout: 10s", "timeout: 31s")
	tests := []struct {
		name, text, path string
		want             int
	}{
		{"below", base, "worker.drain_timeout", 1},
		{"equal", replaceOnce(t, base, "9s", "10s"), "", 0},
		{"above", replaceOnce(t, base, "9s", "11s"), "", 0},
		{"largest listener", larger, "worker.drain_timeout", 1},
		{"defaulted drain", defaulted, "listeners[0].timeout", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, warnings, errs := Parse([]byte(tc.text), "drain.yaml", MapEnv(nil))
			if cfg == nil || len(errs) != 0 || len(warnings) != tc.want {
				t.Fatalf("config=%v warnings=%+v errors=%+v, want %d warning", cfg, warnings, errs, tc.want)
			}
			if tc.want != 0 && warnings[0].Path != tc.path {
				t.Errorf("warning Path = %q, want D2 anchor %q", warnings[0].Path, tc.path)
			}
		})
	}
}

func TestWarningPolicyAcrossACompleteRunAndAStageDStop(t *testing.T) {
	tests := []struct {
		name       string
		wantConfig bool
		wantErrors int
		wantWarns  int
		wantRule   RuleID
	}{
		{"warnings_only", true, 0, 2, noRule},
		{"warnings_plus_semantic_error", false, 1, 2, R1},
		{"warnings_plus_unresolved_reference", false, 1, 0, RuleInterpolate},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := warningFixture(tc.name)
			cfg, warnings, errs := Parse(
				readFixtureBytes(t, path), filepath.Base(path), MapEnv(nil),
			)
			if (cfg != nil) != tc.wantConfig || len(errs) != tc.wantErrors || len(warnings) != tc.wantWarns {
				t.Fatalf("config=%v errors=%+v warnings=%+v", cfg, errs, warnings)
			}
			if tc.wantRule != noRule && errs[0].Rule != tc.wantRule {
				t.Errorf("error Rule = %s, want %s", errs[0].Rule, tc.wantRule)
			}
			if tc.wantWarns == 2 && (!carriesWarning(warnings, W1) || !carriesWarning(warnings, W2)) {
				t.Errorf("warnings %+v do not contain W1 and W2", warnings)
			}
		})
	}
}

func carriesWarning(warnings Warnings, rule RuleID) bool {
	for _, warning := range warnings {
		if warning.Rule == rule {
			return true
		}
	}
	return false
}

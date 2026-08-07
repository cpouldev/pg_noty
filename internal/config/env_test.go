package config

import (
	"os"
	"testing"
)

// EnvLookup must stay drop-in compatible with os.LookupEnv so production code passes the
// standard library function unmodified. The assertion lives here rather than in env.go so
// that env.go itself keeps the L1 promise of importing neither os nor the YAML library.
var _ EnvLookup = os.LookupEnv

func TestMapEnvDistinguishesUnsetFromSetEmpty(t *testing.T) {
	lookup := MapEnv(map[string]string{"SET_EMPTY": "", "SET_VALUE": "secret"})

	tests := []struct {
		name      string
		variable  string
		wantValue string
		wantOK    bool
	}{
		{name: "unset variable is not ok", variable: "MISSING", wantValue: "", wantOK: false},
		{name: "variable set to the empty string is ok", variable: "SET_EMPTY", wantValue: "", wantOK: true},
		{name: "variable set to a value is ok", variable: "SET_VALUE", wantValue: "secret", wantOK: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			value, ok := lookup(tc.variable)
			if value != tc.wantValue || ok != tc.wantOK {
				t.Errorf("lookup(%q) = (%q, %t), want (%q, %t)", tc.variable, value, ok, tc.wantValue, tc.wantOK)
			}
		})
	}
}

func TestMapEnvIgnoresLaterMutationOfItsInput(t *testing.T) {
	vars := map[string]string{"TOKEN": "original"}
	lookup := MapEnv(vars)

	vars["TOKEN"] = "changed"
	delete(vars, "TOKEN")

	if value, ok := lookup("TOKEN"); value != "original" || !ok {
		t.Errorf("lookup(TOKEN) = (%q, %t) after mutating the input map, want (\"original\", true)", value, ok)
	}
}

// TestMapEnvTreatsNoVariablesAsAnEmptyEnvironment covers the environment every test in
// this package loads with: a nil map is a valid environment in which every reference is
// unset, rather than a nil map that a lookup could fault on.
func TestMapEnvTreatsNoVariablesAsAnEmptyEnvironment(t *testing.T) {
	if value, ok := MapEnv(nil)("DATABASE_URL"); value != "" || ok {
		t.Errorf("MapEnv(nil)(\"DATABASE_URL\") = (%q, %t), want (\"\", false)", value, ok)
	}
}

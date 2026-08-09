package config

import (
	"slices"
	"testing"
)

func TestOnlyResolvedLogSecretPathsHaveLogSensitivity(t *testing.T) {
	want := []string{
		"database.listen_url",
		"database.url",
		"listeners[].destination.signing.secrets",
		"listeners[].destination.url",
	}
	got := pathsWhere(t, func(spec keySpec) bool {
		return spec.logSensitivity != publicValue
	})
	if !slices.Equal(got, want) {
		t.Errorf("log-sensitive keys %v, want exactly %v", got, want)
	}
}

func TestDestinationURLDeclaresTheTwoOutputSurfacesAsymmetric(t *testing.T) {
	spec := declaredKeys(t)["listeners[].destination.url"]
	if spec.sensitive != publicValue {
		t.Errorf("diagnostic sensitivity = %d, want public", spec.sensitive)
	}
	if spec.logSensitivity != urlPassword {
		t.Errorf("log sensitivity = %d, want URL password", spec.logSensitivity)
	}
}

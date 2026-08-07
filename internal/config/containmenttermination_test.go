package config

import "testing"

// TestAnUnterminatedFinalLineIsObservedOnBothRedactionBranches is a one-line reproduction of the
// oracle boundary: with no LF at all, counting LF bytes schedules no diagnostic and can make a leak
// search pass without rendering the secret-bearing line.
func TestAnUnterminatedFinalLineIsObservedOnBothRedactionBranches(t *testing.T) {
	secret := secretMarkedOnEveryLine("NO-FINAL-LF")
	connection := yamlQuoted(connectionStringHiding(secret.text, "db.internal"))
	tests := []plantedSecret{
		{
			branch:    "path-aware",
			pathAware: true,
			text:      "{version: 1, database: {url: " + connection + "}}",
			markers:   secret.markers,
		},
		{
			branch:    "key-scoped fallback",
			pathAware: false,
			text:      "{version: 1, database: {url: " + connection,
			markers:   secret.markers,
		},
	}

	for _, planted := range tests {
		t.Run(planted.branch, func(t *testing.T) {
			assertNothingLeaks(t, planted)
		})
	}
}

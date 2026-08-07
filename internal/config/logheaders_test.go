package config

import (
	"strings"
	"testing"
)

// TestHeaderValuesAreNeverLogged holds the decision logHeaders records. A destination header is
// where an operator puts the credential their endpoint expects -- Authorization, an API key, a
// shared token -- and this package's contract is that a secret does not reach an output stream.
// The names survive because "which headers are configured" is what a reader of this log is asking.
//
// Both directions are asserted: a repair that dropped the group entirely would satisfy the first
// clause and destroy the log's usefulness.
func TestHeaderValuesAreNeverLogged(t *testing.T) {
	const credential = "Bearer S3CRET-DESTINATION-TOKEN"
	attr := logHeaders(Headers{"Authorization": credential, "X-Tenant": "acme"})

	logged := attr.Value.String()
	if strings.Contains(logged, credential) || strings.Contains(logged, "S3CRET") {
		t.Fatalf("logged headers %q carry the credential", logged)
	}
	if strings.Contains(logged, "acme") {
		t.Errorf("logged headers %q carry a header value; the schema cannot say which header holds a "+
			"credential, so every value is withheld", logged)
	}
	for _, name := range []string{"Authorization", "X-Tenant"} {
		if !strings.Contains(logged, name) {
			t.Errorf("logged headers %q omit %q; the names are the question this log answers", logged, name)
		}
	}
	if strings.Count(logged, redactionPlaceholder) != 2 {
		t.Errorf("logged headers %q hold %d placeholders, want one per header", logged,
			strings.Count(logged, redactionPlaceholder))
	}
}

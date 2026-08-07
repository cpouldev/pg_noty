package config

import (
	"strings"
	"testing"
)

// A free-form header name can share a spelling with a sensitive key elsewhere.
// The declared free-form path is still public; only a genuinely undeclared parent
// invokes the fail-closed leaf-name fallback.
func TestAFreeFormHeaderNamedLikeASensitiveKeyRemainsPublic(t *testing.T) {
	source := "defaults:\n" +
		"  headers:\n" +
		"    url: defaults-kept\n" +
		"listeners:\n" +
		"- destination:\n" +
		"    headers:\n" +
		"      secrets: destination-kept\n"

	rendered := renderEveryLineOf(source)
	for _, want := range []string{"url: defaults-kept", "secrets: destination-kept"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the path-aware redactor hid declared free-form text %q:\n%s", want, rendered)
		}
	}
}

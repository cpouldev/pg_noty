package config

import (
	"strings"
	"testing"
)

// containedLeakSentinel is written where the schema declares the value secret in full, as a repeated
// mapping key, so the parser composes the key's own text into its message. That message is the
// second rendered channel, and Contained is the only way a caller outside this package can reach the
// redaction that governs it.
const containedLeakSentinel = "S3CRET-SIGNING-KEY"

const documentWhoseParserMessageNamesASecret = `version: 1
database:
  url: postgres://noty@db.internal/noty
listeners:
  - name: signed_hook
    table: public.orders
    operations: [insert]
    destination:
      url: https://hooks.example.test/signed
      signing:
        secrets:
          ` + containedLeakSentinel + `: first
          ` + containedLeakSentinel + `: second
`

// TestContainedRedactsADiagnosticTheSameWayRenderDoes pins the exported half of the second channel.
// Both directions are asserted: the secret must be gone, and the condition the message states must
// survive -- a Contained that returned the empty string, or that replaced the whole message, would
// satisfy the first clause and destroy the diagnostic's usefulness, which is the trade
// quotableLines.contained exists to avoid.
func TestContainedRedactsADiagnosticTheSameWayRenderDoes(t *testing.T) {
	source := []byte(documentWhoseParserMessageNamesASecret)
	_, _, diags := Parse(source, "listeners.yaml", func(string) (string, bool) { return "", false })
	if len(diags) == 0 {
		t.Fatalf("the document parsed clean, so no diagnostic carries %q", containedLeakSentinel)
	}
	if !strings.Contains(joinedDiagnosticText(diags), containedLeakSentinel) {
		t.Fatalf("no raw diagnostic names %q, so this test asserts nothing: %+v",
			containedLeakSentinel, diags)
	}

	contained := diags.Contained(source)
	if len(contained) != len(diags) {
		t.Fatalf("Contained returned %d diagnostics, want the %d it was given",
			len(contained), len(diags))
	}
	if text := joinedDiagnosticText(contained); strings.Contains(text, containedLeakSentinel) {
		t.Fatalf("Contained left the secret in the diagnostic text: %s", text)
	}
	if !strings.Contains(joinedDiagnosticText(contained), redactionPlaceholder) {
		t.Errorf("Contained removed the secret without leaving %s, so a reader cannot tell a "+
			"withheld word from one that was never written: %s",
			redactionPlaceholder, joinedDiagnosticText(contained))
	}
	if !strings.Contains(joinedDiagnosticText(contained), "already defined") {
		t.Errorf("Contained discarded the condition the message states, leaving a reader nothing "+
			"to act on: %s", joinedDiagnosticText(contained))
	}
}

// TestContainedIsANoOpForADocumentDeclaringNothingSecret is the near-miss the guard must survive:
// a diagnostic naming ordinary text must come back byte-identical, or Contained is
// over-redacting.
func TestContainedIsANoOpForADocumentDeclaringNothingSecret(t *testing.T) {
	source := []byte("version: 1\ndatabase:\n  url: postgres://noty@db.internal/noty\n  schema: noty\n  schema: noty\n")
	_, _, diags := Parse(source, "listeners.yaml", func(string) (string, bool) { return "", false })
	if len(diags) == 0 {
		t.Fatal("the near-miss document parsed clean, so nothing is compared")
	}
	before := joinedDiagnosticText(diags)
	if after := joinedDiagnosticText(diags.Contained(source)); after != before {
		t.Errorf("Contained rewrote a diagnostic that names no secret:\nbefore %q\nafter  %q",
			before, after)
	}
}

func joinedDiagnosticText(diags Errors) string {
	var joined strings.Builder
	for _, diag := range diags {
		joined.WriteString(diag.Msg)
		joined.WriteString(" ")
		joined.WriteString(diag.Hint)
		joined.WriteString("\n")
	}
	return joined.String()
}

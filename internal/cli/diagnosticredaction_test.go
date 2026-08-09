package cli

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// theLeakSentinel is written where the schema declares the value secret in full, and it is shaped so
// the parser names it back: a repeated mapping key makes goccy compose the key's own text into its
// message, and that message is the second channel internal/config/diagnostictext.go exists to close.
const theLeakSentinel = "S3CRET-SIGNING-KEY"

const documentHidingASecretInAParserMessage = `version: 1
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
          ` + theLeakSentinel + `: first
          ` + theLeakSentinel + `: second
`

// TestNoDiagnosticRenderingQuotesASecretInAnyFormat holds both output formats to one answer. The
// text path routes every message through Errors.Render, which redacts against the source; the JSON
// path built its records from diag.Msg directly and emitted the parser's message verbatim, so
// `--log-format json` printed a signing secret that `--log-format text` had replaced.
//
// internal/config's own TestNoRenderedOutputPathBypassesTheRedactor cannot see this: it ranges over
// functions in internal/config, and this renderer is in internal/cli.
func TestNoDiagnosticRenderingQuotesASecretInAnyFormat(t *testing.T) {
	cfg, warnings, errs := config.Parse(
		[]byte(documentHidingASecretInAParserMessage), "listeners.yaml",
		func(string) (string, bool) { return "", false },
	)
	loaded := loadedConfig{
		Value: cfg, Source: []byte(documentHidingASecretInAParserMessage),
		Warnings: warnings, Errors: errs,
	}
	if len(errs) == 0 {
		t.Fatalf(
			"the document parsed clean, so no diagnostic carries %q and this test asserts nothing",
			theLeakSentinel,
		)
	}
	if !strings.Contains(diagnosticText(errs), theLeakSentinel) {
		t.Fatalf(
			"no diagnostic message names %q, so the leak channel this test guards is not reached; "+
				"diagnostics were %+v", theLeakSentinel, errs,
		)
	}

	for _, format := range []string{"text", "json"} {
		t.Run(
			format, func(t *testing.T) {
				rendered, err := renderLoadedDiagnostics(loaded, format)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(rendered, theLeakSentinel) {
					t.Fatalf("--log-format %s rendered the secret verbatim:\n%s", format, rendered)
				}
			},
		)
	}
}

// diagnosticText is the raw, unredacted text the renderers are handed, used only to prove the
// fixture reaches the channel under test.
func diagnosticText(diags config.Errors) string {
	var joined strings.Builder
	for _, diag := range diags {
		joined.WriteString(diag.Msg)
		joined.WriteString(diag.Hint)
	}
	return joined.String()
}

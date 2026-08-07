package config

import "testing"

// TestParseReturnsStageHSemanticDiagnostics pins that a semantic refusal reaches the caller from
// Parse, with no configuration and no warnings beside it.
func TestParseReturnsStageHSemanticDiagnostics(t *testing.T) {
	text := stageHExtensionSafeValue("  concurrency: 8\n")
	text = replaceOnce(t, text, "version: 1", "version: 2")

	cfg, warnings, diags := Parse([]byte(text), "listeners.yaml", corpusEnvironment())
	if len(diags) != 1 || diags[0].Rule != R1 {
		t.Fatalf("Parse returned %q, want the one R1 value diagnostic", messagesOf(diags))
	}
	if cfg != nil || len(warnings) != 0 {
		t.Errorf("Parse returned config %v and warnings %v alongside a semantic diagnostic", cfg, warnings)
	}
}

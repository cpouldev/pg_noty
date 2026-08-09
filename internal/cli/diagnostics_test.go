package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func TestDiagnosticsRenderTextAndJSONWithWarningAndError(t *testing.T) {
	loaded := loadedConfig{
		Source: []byte("version: 1\n"), Errors: config.Errors{config.NewFileError(config.RuleRead, "x.yaml", "bad")},
		Warnings: config.Warnings{config.Warning(config.NewFileError(config.RuleRead, "x.yaml", "warn"))},
	}
	text, err := renderLoadedDiagnostics(loaded, "text")
	if err != nil || !strings.Contains(text, "bad") || !strings.Contains(text, "warn") {
		t.Fatalf("text diagnostics = %q, err=%v", text, err)
	}
	encoded, err := renderLoadedDiagnostics(loaded, "json")
	if err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	if err := json.Unmarshal([]byte(encoded), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0]["kind"] != "error" || records[1]["kind"] != "warning" {
		t.Fatalf("JSON diagnostics = %#v", records)
	}
}

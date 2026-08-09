package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cpouldev/pg_noty/internal/config"
)

func renderLoadedDiagnostics(loaded loadedConfig, format string) (string, error) {
	if strings.EqualFold(format, "json") {
		return renderDiagnosticsJSON(loaded)
	}
	return loaded.Errors.Render(loaded.Source) + loaded.Warnings.Render(loaded.Source), nil
}

func renderDiagnosticsJSON(loaded loadedConfig) (string, error) {
	type record struct {
		Kind string `json:"kind"`
		File string `json:"file"`
		Line int    `json:"line"`
		Col  int    `json:"column"`
		Path string `json:"path"`
		Msg  string `json:"message"`
		Hint string `json:"hint,omitempty"`
	}
	// Contained is what the text path gets from Render. Reading Error.Msg directly here emitted the
	// parser's own message verbatim, and the parser composes that out of the document -- so
	// `--log-format json` printed a secret that `--log-format text` had already replaced.
	warnings := make(config.Errors, 0, len(loaded.Warnings))
	for _, diag := range loaded.Warnings {
		warnings = append(warnings, config.Error(diag))
	}
	rows := make([]record, 0, len(loaded.Errors)+len(loaded.Warnings))
	for _, diag := range loaded.Errors.Contained(loaded.Source) {
		rows = append(rows, record{"error", diag.File, diag.Line, diag.Col, diag.Path, diag.Msg, diag.Hint})
	}
	for _, diag := range warnings.Contained(loaded.Source) {
		rows = append(rows, record{"warning", diag.File, diag.Line, diag.Col, diag.Path, diag.Msg, diag.Hint})
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return "", fmt.Errorf("render diagnostics: %w", err)
	}
	return string(data) + "\n", nil
}

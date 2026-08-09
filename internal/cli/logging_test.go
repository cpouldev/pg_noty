package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"

	"github.com/spf13/cobra"
)

func TestLoggingFormatsLevelsAndRefusals(t *testing.T) {
	for _, level := range documentedLogLevels {
		var jsonOutput bytes.Buffer
		if status := executeLoggingTest(t, level, "json", &jsonOutput); status != 0 {
			t.Fatalf("json level %s status = %d", level, status)
		}
		var record map[string]any
		if err := json.Unmarshal(jsonOutput.Bytes(), &record); err != nil {
			t.Fatalf("json level %s did not parse: %v (%q)", level, err, jsonOutput.String())
		}
		var textOutput bytes.Buffer
		if status := executeLoggingTest(t, level, "text", &textOutput); status != 0 {
			t.Fatalf("text level %s status = %d", level, status)
		}
		if json.Valid(textOutput.Bytes()) {
			t.Fatalf("text level %s unexpectedly parsed as JSON", level)
		}
	}
	for _, flag := range []string{"--log-level", "--log-format"} {
		var output bytes.Buffer
		worked := false
		registerCommand("logging-refusal", func(opts *rootOptions) *cobra.Command {
			return &cobra.Command{Use: "logging-refusal", RunE: func(*cobra.Command, []string) error { worked = true; return nil }}
		})
		defer unregisterCommand("logging-refusal")
		status := Execute(t.Context(), []string{flag, "invalid", "logging-refusal"}, Streams{Out: &output, Err: &output})
		if status == 0 || worked {
			t.Fatalf("invalid %s status=%d worked=%t output=%q", flag, status, worked, output.String())
		}
	}
	if _, err := parseLogLevel("warning"); err == nil {
		t.Fatal("undocumented log-level alias warning was accepted")
	}
}

func executeLoggingTest(t *testing.T, level, format string, output *bytes.Buffer) int {
	t.Helper()
	word := fmt.Sprintf("logging-probe-%p", output)
	registerCommand(word, func(opts *rootOptions) *cobra.Command {
		return &cobra.Command{Use: word, RunE: func(cmd *cobra.Command, _ []string) error {
			refuseCommand(cmd)
			logger, err := requireLogger(opts)
			if err != nil {
				return err
			}
			logger.Log(t.Context(), levelForTest(level), "probe", "level", level)
			return nil
		}}
	})
	defer unregisterCommand(word)
	return Execute(t.Context(), []string{"--log-level", level, "--log-format", format, word}, Streams{Out: output, Err: output})
}

func levelForTest(level string) slog.Level {
	parsed, _ := parseLogLevel(level)
	return parsed
}

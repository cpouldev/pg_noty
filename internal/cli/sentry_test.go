package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/reconcile"
	"github.com/getsentry/sentry-go"
	"github.com/spf13/cobra"
)

func TestExecuteReportsOnlyUnexpectedFailuresToSentry(t *testing.T) {
	testCases := []struct {
		name       string
		commandErr error
		wantStatus int
		wantEvents int
	}{
		{name: "success", wantStatus: 0, wantEvents: 0},
		{name: "unexpected", commandErr: errors.New("unexpected Sentry probe"), wantStatus: 1, wantEvents: 1},
		{name: "outcome", commandErr: outcomeError{verdict: reconcile.VerdictChangesPending}, wantStatus: 2, wantEvents: 0},
		{name: "wrapped outcome", commandErr: fmt.Errorf("wrapped: %w", outcomeError{verdict: reconcile.VerdictChangesPending}), wantStatus: 2, wantEvents: 0},
	}
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			hub, transport := newTestSentryHub(t)
			ctx := sentry.SetHubOnContext(t.Context(), hub)

			word := fmt.Sprintf("sentry-probe-%d", index)
			ran := false
			registerCommand(word, func(*rootOptions) *cobra.Command {
				return &cobra.Command{Use: word, RunE: func(cmd *cobra.Command, _ []string) error {
					ran = true
					refuseCommand(cmd)
					return testCase.commandErr
				}}
			})
			defer unregisterCommand(word)

			var output bytes.Buffer
			if status := Execute(ctx, []string{word}, Streams{Out: &output, Err: &output}); status != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; output=%q", status, testCase.wantStatus, output.String())
			}
			if !ran {
				t.Fatal("probe command did not run")
			}
			events := transport.Events()
			if len(events) != testCase.wantEvents {
				t.Fatalf("captured %d events, want %d", len(events), testCase.wantEvents)
			}
			if testCase.wantEvents == 1 {
				if len(events[0].Exception) == 0 || events[0].Exception[0].Value != testCase.commandErr.Error() {
					t.Fatalf("captured event = %#v, want command error", events[0])
				}
				if !strings.Contains(output.String(), testCase.commandErr.Error()) {
					t.Fatalf("stderr = %q, want the same reported failure", output.String())
				}
			} else if output.Len() != 0 {
				t.Fatalf("outcome stderr = %q, want it silent", output.String())
			}
		})
	}
}

func TestExecuteReportsBackgroundErrorLogsToSentry(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			hub, transport := newTestSentryHub(t)
			ctx := sentry.SetHubOnContext(t.Context(), hub)
			const word = "sentry-worker-probe"
			const secret = "SENTRY-LOG-SECRET"
			cfg := config.Config{
				Instance: "sentry-test-instance",
				Database: config.Database{URL: "postgres://noty:" + secret + "@localhost/noty"},
			}
			registerCommand(word, func(opts *rootOptions) *cobra.Command {
				return &cobra.Command{Use: word, RunE: func(cmd *cobra.Command, _ []string) error {
					refuseCommand(cmd)
					logger := opts.Logger.With("component", "delivery").
						WithGroup("worker").With("config", cfg)
					done := make(chan struct{})
					go func() {
						defer close(done)
						logger.Debug("worker debug")
						logger.Info("worker info")
						logger.Warn("worker warning")
						logger.Error("worker stopped", "error", errors.New("queue unavailable"), "attempt", 2, "event_id", int64(9007199254740993))
					}()
					<-done
					return nil
				}}
			})
			defer unregisterCommand(word)

			var output bytes.Buffer
			status := Execute(ctx, []string{word, "--log-format", format, "--log-level", "debug"}, Streams{Out: &output, Err: &output})
			if status != 0 {
				t.Fatalf("status = %d, output = %q", status, output.String())
			}
			events := transport.Events()
			if len(events) != 1 {
				t.Fatalf("captured %d events, want only the background error", len(events))
			}
			event := events[0]
			if event.Message != "worker stopped" || event.Level != sentry.LevelError {
				t.Fatalf("event = %#v, want the worker error", event)
			}
			if len(event.Threads) == 0 || event.Threads[0].Stacktrace == nil {
				t.Fatal("background error has no stacktrace")
			}
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), secret) || strings.Contains(output.String(), secret) {
				t.Fatal("Sentry event or local log exposed the configuration password")
			}
			for _, text := range []string{"worker stopped", "queue unavailable", "sentry-test-instance", "delivery", "worker", "attempt", "9007199254740993"} {
				if !strings.Contains(string(encoded), text) || !strings.Contains(output.String(), text) {
					t.Fatalf("missing %q from event %s or log %q", text, encoded, output.String())
				}
			}
			for _, message := range []string{"worker debug", "worker info", "worker warning"} {
				if !strings.Contains(output.String(), message) {
					t.Fatalf("Sentry suppressed the local log %q", message)
				}
			}
		})
	}
}

func newTestSentryHub(t *testing.T) (*sentry.Hub, *sentry.MockTransport) {
	t.Helper()
	previousLogger := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	transport := &sentry.MockTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:              testSentryDSN,
		AttachStacktrace: true,
		DisableLogs:      true,
		DisableMetrics:   true,
		Transport:        transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return sentry.NewHub(client, sentry.NewScope()), transport
}

const testSentryDSN = "https://public@example.com/1"

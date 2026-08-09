//go:build integration

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/delivery"
)

func TestComposeColdStartProducesOneSignedWebhook(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker daemon unavailable")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	project := fmt.Sprintf("pgnoty-compose-%d", os.Getpid())
	runCompose := func(args ...string) ([]byte, error) {
		command := exec.Command("docker", append([]string{"compose", "-p", project}, args...)...)
		command.Dir = root
		command.Env = append(os.Environ(), "GO_VERSION="+moduleGoVersion(t, root))
		return command.CombinedOutput()
	}
	_, _ = runCompose("down", "-v", "--remove-orphans")
	t.Cleanup(func() { _, _ = runCompose("down", "-v", "--remove-orphans") })
	if output, err := runCompose("up", "-d", "--build"); err != nil {
		t.Fatalf("cold compose up failed: %v\n%s", err, output)
	}
	awaitWithin(
		t, "pg_noty to report ready", stackStartBudget, func() bool {
			response, requestErr := http.Get("http://127.0.0.1:8080/readyz")
			if requestErr != nil {
				return false
			}
			_ = response.Body.Close()
			return response.StatusCode == http.StatusOK
		},
	)
	awaitWithin(
		t, "postgres to accept the seed INSERT", stackStartBudget, func() bool {
			_, err := runCompose(
				"exec", "-T", "postgres", "psql", "-U", "noty", "-d", "noty", "-c",
				"INSERT INTO public.orders (customer,total_cents) VALUES ('Ada',4200);",
			)
			return err == nil
		},
	)

	// The documented webhook window, and the only budget with a published bound: 30 seconds measured
	// from the INSERT, not from the start of the stack. Sharing one clock with the two phases above
	// made this budget whatever they left over, so a slow start was reported as a missing webhook and
	// the documented claim was never actually measured.
	var normalized string
	awaitWithin(
		t, "the echo server to log exactly one webhook", webhookBudgetAfterInsert, func() bool {
			output, err := runCompose("logs", "--no-color", "echo")
			normalized = stripComposePrefix(string(output))
			return err == nil && strings.Count(normalized, `"method": "POST"`) == 1
		},
	)
	assertEchoRequest(t, normalized)
}

const (
	// webhookBudgetAfterInsert is the documented window's literal, and it starts at the INSERT.
	webhookBudgetAfterInsert = 30 * time.Second
	// stackStartBudget covers bringing the stack up. It is deliberately not the webhook window's
	// number: nothing documented bounds container start, and this test shares a daemon with five
	// other packages' testcontainers under `go test -tags integration ./...`, where a 30-second
	// start budget failed while the same stack came ready in four seconds when run alone. A start
	// budget that only holds when the suite is run one package at a time is a gate that passes for
	// the author and fails for everyone else.
	stackStartBudget = 180 * time.Second
)

// awaitWithin gives one phase its own budget. Every phase used to share a single deadline computed
// before the first of them, so a slow phase silently consumed the budget of the phases after it and
// the failure was reported against whichever one ran out rather than the one that was slow.
func awaitWithin(t *testing.T, what string, budget time.Duration, satisfied func() bool) {
	t.Helper()
	started := time.Now()
	for time.Since(started) < budget {
		if satisfied() {
			t.Logf("waited %s for %s", time.Since(started).Round(time.Millisecond), what)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", budget, what)
}

func stripComposePrefix(output string) string {
	lines := strings.Split(output, "\n")
	for index, line := range lines {
		if marker := strings.Index(line, " | "); marker >= 0 {
			lines[index] = line[marker+3:]
		}
	}
	return strings.Join(lines, "\n")
}

func assertEchoRequest(t *testing.T, output string) {
	t.Helper()
	start := strings.Index(output, "{\n")
	if start < 0 {
		t.Fatalf("echo log has no request JSON:\n%s", output)
	}
	var request struct {
		Headers map[string]string `json:"headers"`
		Method  string            `json:"method"`
		Body    string            `json:"body"`
	}
	if err := json.NewDecoder(strings.NewReader(output[start:])).Decode(&request); err != nil {
		t.Fatal(err)
	}
	if request.Method != "POST" || request.Headers["x-pg-noty-event-id"] == "" || request.Headers["x-pg-noty-attempt"] == "" {
		t.Fatalf("echo request lacks the delivery envelope headers: %+v", request.Headers)
	}
	timestamp, err := strconv.ParseInt(request.Headers["x-pg-noty-timestamp"], 10, 64)
	if err != nil || !delivery.VerifyHeader(
		[]byte("demo-secret"),
		timestamp,
		[]byte(request.Body),
		request.Headers["x-pg-noty-signature"],
	) {
		t.Fatalf("echo request signature did not verify: %+v", request.Headers)
	}
	var envelope map[string]any
	if err := json.Unmarshal(
		[]byte(request.Body),
		&envelope,
	); err != nil || envelope["id"] == nil || envelope["listener"] != "orders" {
		t.Fatalf("echo request body is not the documented envelope: %s", request.Body)
	}
}

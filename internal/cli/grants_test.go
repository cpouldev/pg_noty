package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestGrantsUsesLibraryStatementsAndRoleBoundary(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@db.invalid/noty")
	t.Setenv("ORDER_WEBHOOK_URL", "https://hooks.invalid/order")
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := []byte("version: 1\ndatabase:\n  url: ${DATABASE_URL}\nlisteners:\n  - name: one\n    table: public.orders\n    operations: [insert]\n    destination:\n      url: ${ORDER_WEBHOOK_URL}\n  - name: two\n    table: public.orders\n    operations: [update]\n    destination:\n      url: ${ORDER_WEBHOOK_URL}\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	status := Execute(
		t.Context(),
		[]string{"--config", path, "grants", "--role", "noty"},
		Streams{Out: &output, Err: &output},
	)
	if status != 0 {
		t.Fatalf("grants status = %d (%s)", status, output.String())
	}
	loaded := loadConfig(path)
	want := schema.GrantStatements(loaded.Value.Database.Schema, "noty", []string{"public.orders"})
	if got := strings.TrimSpace(output.String()); got != strings.Join(want, "\n") {
		t.Fatalf("grant output = %q, want %q", got, strings.Join(want, "\n"))
	}
	if got := configuredTargets(loaded.Value); !slices.Equal(got, []string{"public.orders"}) {
		t.Fatalf("targets = %v, want one distinct table", got)
	}
	for _, role := range []string{"pg_", "pg_a"} {
		output.Reset()
		status := Execute(
			t.Context(),
			[]string{"--config", path, "grants", "--role", role},
			Streams{Out: &output, Err: &output},
		)
		// The claim is that no privilege is emitted for a reserved role. It used to be spelled
		// "nothing was written at all", which held only because a failed command reported no reason
		// for failing; now that it does, the emptiness proxy would refuse the refusal itself.
		if status == 0 || strings.Contains(output.String(), "GRANT") || strings.Contains(output.String(), "ALTER") {
			t.Fatalf("reserved role %q status=%d output=%q", role, status, output.String())
		}
		if !strings.Contains(output.String(), role) {
			t.Errorf("the refusal for %q does not name the role: %q", role, output.String())
		}
	}
	if status := Execute(
		t.Context(),
		[]string{"--config", path, "grants", "--role", "pg"},
		Streams{Out: ioDiscard{}, Err: ioDiscard{}},
	); status != 0 {
		t.Fatalf("short role pg status = %d, want admitted", status)
	}
}

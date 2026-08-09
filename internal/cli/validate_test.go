package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateUsesNoSocketAndDefinesFailureStatuses(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://user:pass@db.invalid/noty")
	t.Setenv("ORDER_WEBHOOK_URL", "https://hooks.invalid/order")
	valid := filepath.Join(t.TempDir(), "valid.yaml")
	if err := os.WriteFile(valid, []byte("version: 1\ndatabase:\n  url: ${DATABASE_URL}\nlisteners:\n  - name: order_paid\n    table: public.orders\n    operations: [insert]\n    destination:\n      url: ${ORDER_WEBHOOK_URL}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := Execute(t.Context(), []string{"--config", valid, "validate"}, Streams{Out: ioDiscard{}, Err: ioDiscard{}}); status != 0 {
		t.Fatalf("valid validate status = %d", status)
	}
	invalid := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(invalid, []byte("version: 1\ndatabase:\n  url: postgres://x\nlisteners:\n  - name: BAD!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if status := Execute(t.Context(), []string{"--config", invalid, "validate"}, Streams{Out: ioDiscard{}, Err: ioDiscard{}}); status == 0 || status == 2 {
		t.Fatalf("invalid validate status = %d, want defined non-zero non-pending", status)
	}
	if status := Execute(t.Context(), []string{"--config", filepath.Join(t.TempDir(), "missing"), "validate"}, Streams{Out: ioDiscard{}, Err: ioDiscard{}}); status == 0 || status == 2 {
		t.Fatalf("missing validate status = %d, want defined non-zero non-pending", status)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(data []byte) (int, error) { return len(data), nil }

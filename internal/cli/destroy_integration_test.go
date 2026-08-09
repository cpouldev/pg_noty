//go:build integration

package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestDestroyMixedOwnershipAndCounts(t *testing.T) {
	pool, _, path := cliDatabase(t, true)
	var planOutput bytes.Buffer
	if got := Execute(
		t.Context(),
		[]string{"--config", path, "apply", "--auto-approve"},
		Streams{Out: &planOutput, Err: &planOutput},
	); got != 0 {
		t.Fatalf("seed apply status=%d output=%s", got, planOutput.String())
	}
	var trigger string
	if err := pool.QueryRow(
		t.Context(),
		"SELECT trigger_name FROM noty.listener_triggers WHERE listener='orders' AND operation='insert'",
	).Scan(&trigger); err != nil {
		t.Fatal(err)
	}
	marker := schema.MarkerPrefix + "foreign:orders:insert"
	if _, err := pool.Exec(
		t.Context(),
		fmt.Sprintf("COMMENT ON TRIGGER %s ON public.orders IS '%s'", quoteCLIIdentifier(trigger), marker),
	); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	status := Execute(
		t.Context(),
		[]string{"--config", path, "destroy", "--confirm", "--allow-delete"},
		Streams{Out: &output, Err: &output},
	)
	if status == 0 || !strings.Contains(output.String(), "foreign") || !strings.Contains(
		output.String(),
		"queue pending=0",
	) {
		t.Fatalf("destroy status=%d output=%s", status, output.String())
	}
	var stillThere bool
	if err := pool.QueryRow(
		t.Context(),
		"SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname=$1)",
		trigger,
	).Scan(&stillThere); err != nil {
		t.Fatal(err)
	}
	if !stillThere {
		t.Fatal("foreign trigger was removed")
	}
}

func quoteCLIIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }

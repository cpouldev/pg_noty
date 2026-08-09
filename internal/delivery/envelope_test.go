package delivery

import (
	"bytes"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
)

func TestBuildEnvelopeIsStableAcrossProcesses(t *testing.T) {
	if os.Getenv("PGNOTY_ENVELOPE_HELPER") == "1" {
		body, err := BuildEnvelope(stableEnvelopeEvent())
		if err != nil {
			t.Fatal(err)
		}
		_, _ = os.Stdout.Write(body)
		return
	}
	local, err := BuildEnvelope(stableEnvelopeEvent())
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=TestBuildEnvelopeIsStableAcrossProcesses")
	command.Env = append(os.Environ(), "PGNOTY_ENVELOPE_HELPER=1")
	other, err := command.Output()
	if err != nil {
		t.Fatalf("helper process: %v", err)
	}
	other = bytes.TrimSuffix(other, []byte("PASS\n"))
	if !bytes.Equal(local, other) {
		t.Fatalf("cross-process body differs: %q / %q", local, other)
	}
}

func stableEnvelopeEvent() source.Event {
	return source.Event{
		ID: 7, Listener: "payments", Operation: "insert", Table: `"public"."orders"`,
		OccurredAt: time.Date(2026, 7, 25, 17, 0, 0, 123456000, time.UTC), TXID: 8, Payload: []byte(`{"new":{"id":7}}`),
	}
}

func TestBuildEnvelopePreservesPayloadAndOmitsAttempt(t *testing.T) {
	qualified, fault := schema.Qualified(`audit.schema`, `order."history`)
	if fault != schema.IdentifierOK {
		t.Fatalf("qualified hostile table: %s", fault)
	}
	event := source.Event{
		ID: 42, Listener: `paid"listener`, Operation: "update", Table: qualified,
		OccurredAt: time.Date(2026, 7, 25, 17, 0, 0, 123456000, time.FixedZone("offset", 2*60*60)),
		TXID:       84213, Payload: []byte(`{"new":{"z":"unicode-✓","a":"quote\\\""},"old":null}`), Attempt: 3,
	}
	want := `{"pg_noty":"1","id":42,"listener":"paid\"listener","table":{"schema":"audit.schema","name":"order.\"history"},"op":"update","occurred_at":"2026-07-25T15:00:00.123456Z","txid":84213,"data":{"new":{"z":"unicode-✓","a":"quote\\\""},"old":null}}`
	body, err := BuildEnvelope(event)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != want {
		t.Fatalf("body = %s, want %s", body, want)
	}
	if bytes.Contains(body, []byte(`"attempt"`)) {
		t.Fatal("attempt metadata reached the body")
	}
	second, err := BuildEnvelope(event)
	if err != nil || !bytes.Equal(body, second) {
		t.Fatalf("repeated body differs: %q / %q (%v)", body, second, err)
	}
}

func TestBuildEnvelopeUsesMicrosecondZerosAndRejectsBadInputs(t *testing.T) {
	qualified, fault := schema.Qualified("public", "orders")
	if fault != schema.IdentifierOK {
		t.Fatal(fault)
	}
	event := source.Event{
		ID: 1, Table: qualified, OccurredAt: time.Date(2026, 7, 25, 17, 0, 0, 123450000, time.UTC),
		Payload: []byte(`null`),
	}
	body, err := BuildEnvelope(event)
	if err != nil || !bytes.Contains(body, []byte(".123450Z")) {
		t.Fatalf("microsecond zero was trimmed: %s (%v)", body, err)
	}
	event.Table = `"public".orders`
	if _, err := BuildEnvelope(event); err == nil {
		t.Fatal("unquoted qualified table was accepted")
	}
	event.Table = qualified
	event.Payload = []byte(`{"broken"`)
	if _, err := BuildEnvelope(event); err == nil {
		t.Fatal("invalid payload was accepted")
	}
	event.Payload = nil
	if _, err := BuildEnvelope(event); err == nil {
		t.Fatal("empty payload was silently converted")
	}
}

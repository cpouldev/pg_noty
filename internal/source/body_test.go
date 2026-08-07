package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func bodyRequest(payload config.Payload) Request {
	return Request{
		Instance: "prod", ServiceSchema: "noty",
		Listener: config.Listener{Name: "order_paid", Trigger: config.TriggerSpec{Payload: payload}},
		Target:   Target{Schema: "public", Table: "orders", PrimaryKeyColumns: []string{"id"}},
	}
}

func TestTriggerBodyCarriesTheCompositeEventKeyAndOneClock(t *testing.T) {
	body, err := triggerBody(bodyRequest(config.Payload{Mode: "full"}), config.Operation{Kind: "insert"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"RETURNING id, occurred_at", "(event_id, occurred_at, listener", "SELECT e.id, e.occurred_at",
		"next_attempt_at)", "e.occurred_at FROM e",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if got := strings.Count(body, "clock_timestamp()"); got != 1 {
		t.Errorf("clock_timestamp count = %d, want 1", got)
	}
}

func TestTriggerBodyIsFailClosedAcrossTheOperationModeGrid(t *testing.T) {
	for _, operation := range payloadGridOperations {
		for _, mode := range payloadGridModes {
			payload := mode.payload
			body, err := triggerBody(bodyRequest(payload), config.Operation{Kind: operation})
			if err != nil {
				t.Fatalf("%s/%s: %v", operation, mode.name, err)
			}
			if strings.Contains(body, "EXCEPTION") || !strings.Contains(body, "RETURN NULL") {
				t.Errorf("%s/%s body fail-closed shape = %q", operation, mode.name, body)
			}
		}
	}
}

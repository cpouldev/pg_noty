package cli

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/delivery"
)

func TestListenerLogValueRedactsCredentials(t *testing.T) {
	listener := newListenerLogValue(
		delivery.ListenerConfig{
			Name: "n", URL: "https://user:password@example.invalid/a",
			Headers: map[string]string{"Authorization": "secret"},
		},
	)
	if strings.Contains(listener.Destination, "password") || len(listener.HeaderNames) != 1 {
		t.Fatalf("listener value was not redacted at construction: %#v", listener)
	}
}

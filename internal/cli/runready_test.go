package cli

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadinessNamesEachConditionAndLatchStates(t *testing.T) {
	latch := &reconcileLatch{}
	if latch.ready() {
		t.Fatal("pending latch reported ready")
	}
	latch.resolve(errors.New("startup"))
	if latch.ready() {
		t.Fatal("failed latch reported ready")
	}
	latch.resolve(nil)
	if !latch.ready() {
		t.Fatal("discharged latch not ready")
	}
	ready := readiness{conditions: []readinessCondition{{name: "broken", timeout: 0, check: func(context.Context) error { return errors.New("broken") }}}}
	response := httptest.NewRecorder()
	ready.handler().ServeHTTP(response, httptest.NewRequest("GET", "/readyz", nil))
	if response.Code == 200 || !strings.Contains(response.Body.String(), "broken") {
		t.Fatalf("readiness response = %d %q", response.Code, response.Body.String())
	}
}

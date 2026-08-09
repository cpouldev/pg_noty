package config

import (
	"reflect"
	"testing"
	"time"
)

// TestConfigExposesNoDefaultsField pins that defaults are consumed completely by stage I.
func TestConfigExposesNoDefaultsField(t *testing.T) {
	if _, found := reflect.TypeOf(Config{}).FieldByName("Defaults"); found {
		t.Error("Config exposes a Defaults field; defaults are consumed by the merge stage")
	}
}

// TestListenerSeparatesTriggerFromDelivery pins ADR-8. internal/reconcile hashes exactly
// Listener.Trigger, so a flat listener would put signing secrets into spec_hash.
func TestListenerSeparatesTriggerFromDelivery(t *testing.T) {
	triggerAffecting := []string{"Table", "Operations", "Payload"}
	deliveryAffecting := []string{"Destination", "Retry", "Timeout", "Concurrency"}

	listener := reflect.TypeOf(Listener{})
	for _, field := range append(triggerAffecting, deliveryAffecting...) {
		if _, found := listener.FieldByName(field); found {
			t.Errorf("Listener exposes %s directly; it belongs under Trigger or Delivery", field)
		}
	}
	for _, field := range triggerAffecting {
		if _, found := reflect.TypeOf(TriggerSpec{}).FieldByName(field); !found {
			t.Errorf("TriggerSpec has no %s field", field)
		}
	}
	for _, field := range deliveryAffecting {
		if _, found := reflect.TypeOf(DeliverySpec{}).FieldByName(field); !found {
			t.Errorf("DeliverySpec has no %s field", field)
		}
	}
}

// TestOperationsIsAnOrderedSliceRatherThanAMap pins the Determinism NFR and spec_hash stability.
func TestOperationsIsAnOrderedSliceRatherThanAMap(t *testing.T) {
	operations := reflect.TypeOf(Operations{})
	if operations.Kind() != reflect.Slice {
		t.Fatalf("Operations is a %s, want a slice", operations.Kind())
	}
	if got := operations.Elem(); got != reflect.TypeOf(Operation{}) {
		t.Errorf("Operations elements are %s, want config.Operation", got)
	}
}

func TestHeadersMapCanonicalNamesToValues(t *testing.T) {
	headers := reflect.TypeOf(Headers{})
	if headers.Kind() != reflect.Map {
		t.Fatalf("Headers is a %s, want a map", headers.Kind())
	}
	if headers.Key().Kind() != reflect.String || headers.Elem().Kind() != reflect.String {
		t.Errorf("Headers maps %s to %s, want string to string", headers.Key(), headers.Elem())
	}
}

func TestDurationFieldsUseTheStandardLibraryDuration(t *testing.T) {
	if reflect.TypeOf(Worker{}.PollInterval) != reflect.TypeOf(time.Duration(0)) {
		t.Error("Worker.PollInterval is not a time.Duration")
	}
}

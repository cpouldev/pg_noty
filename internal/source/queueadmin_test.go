package source

import (
	"reflect"
	"strings"
	"testing"
)

func TestQueueAdminAndEventSourceMethodSetsArePinned(t *testing.T) {
	if got := reflect.TypeOf((*EventSource)(nil)).Elem().NumMethod(); got != 6 {
		t.Fatalf("EventSource methods = %d, want 6", got)
	}
	if got := reflect.TypeOf((*QueueAdmin)(nil)).Elem().NumMethod(); got != 3 {
		t.Fatalf("QueueAdmin methods = %d, want 3", got)
	}
	for _, name := range []string{"ObserveQueue", "ListEvents", "RetryBatch"} {
		if _, ok := reflect.TypeOf((*QueueAdmin)(nil)).Elem().MethodByName(name); !ok {
			t.Errorf("QueueAdmin lacks %s", name)
		}
	}
}

func TestRetrySelectorRejectsEmptyAndConflictingForms(t *testing.T) {
	id := int64(7)
	for _, tc := range []struct {
		name string
		got  QueueSelector
		want string
	}{
		{"empty", QueueSelector{}, "requires a selector"},
		{"conflicting", QueueSelector{ID: &id, Listener: "orders", Status: "dead"}, "cannot be combined"},
		{"listener without status", QueueSelector{Listener: "orders"}, "requires both"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := selectorPredicate(tc.got, 2, true); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("selector error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestValidSelectorsAndListAbsenceRemainUsable(t *testing.T) {
	id := int64(7)
	for _, selector := range []QueueSelector{{ID: &id}, {Listener: "orders", Status: "dead"},
		{Listener: "orders"}, {Status: "dead"}, {}} {
		if _, _, err := selectorPredicate(selector, 1, false); err != nil {
			t.Fatalf("list selector %+v refused: %v", selector, err)
		}
	}
	if _, _, err := selectorPredicate(QueueSelector{Status: "unknown"}, 1, false); err == nil {
		t.Fatal("unknown status was accepted")
	}
}

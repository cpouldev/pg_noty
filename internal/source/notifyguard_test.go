package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// guardedBodyLandmarks are the four body lines whose *order* is the notify guard. Presence is not
// the property: a body that writes the IF and then puts PERFORM pg_notify after END IF holds every
// one of these strings and notifies once per row instead of once per transaction. Each is counted
// as well as located, so a second notification written outside the block fails the count rather
// than hiding behind the first one's position.
var guardedBodyLandmarks = []struct{ name, text string }{
	{"the guard", "IF coalesce(current_setting('pg_noty.n', true), '') = '' THEN"},
	{"the notification", "PERFORM pg_notify("},
	{"the transaction-local key", "PERFORM set_config('pg_noty.n', '1', true)"},
	{"the guard's end", "END IF;"},
}

// bodyLineHolding is the index of the first body line holding want, and how many lines hold it.
// The not-found answer is -1 rather than 0, which is a legal index and would read as "the first
// line".
func bodyLineHolding(body, want string) (index, count int) {
	index = -1
	for number, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, want) {
			continue
		}
		count++
		if index < 0 {
			index = number
		}
	}
	return index, count
}

func measuredNotifyBody(t *testing.T) string {
	t.Helper()
	body, err := triggerBody(
		Request{
			ServiceSchema: "noty", Listener: config.Listener{
				Name:    "orders",
				Trigger: config.TriggerSpec{Payload: config.Payload{Mode: "full"}},
			},
			Target: Target{Schema: "public", Table: "orders"},
		}, config.Operation{Kind: "insert"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// M8 measured current_setting(... ) IS NULL as 1,0,0 notifications across three transactions on
// one connection: it notifies once per connection. The transaction-local empty-string reset needs
// the coalesce guard emitted below.
func TestNotifyGuardIsTheMeasuredTransactionForm(t *testing.T) {
	body := measuredNotifyBody(t)
	want := "coalesce(current_setting('pg_noty.n', true), '') = ''"
	if !strings.Contains(body, want) || strings.Contains(body, "current_setting('pg_noty.n', true) IS NULL") {
		t.Fatalf("notify guard in body = %q, want %q and no IS NULL form", body, want)
	}
	if !strings.Contains(body, "set_config('pg_noty.n', '1', true)") {
		t.Error("notify guard does not set the transaction-local key")
	}
}

// TestTheNotifyGuardEnclosesTheNotificationAndTheKeyItSets is the guard's own property: the
// notification and the key that suppresses the next one are written *between* the IF and its
// END IF. The measured form above cannot see this -- it asks only whether the guard's text is
// somewhere in the body -- so a body that emits the IF and notifies after END IF satisfies it
// while notifying once per row.
func TestTheNotifyGuardEnclosesTheNotificationAndTheKeyItSets(t *testing.T) {
	lines := guardedLandmarkLines(t, measuredNotifyBody(t))
	opens, closes := lines[0], lines[3]

	for _, enclosed := range []int{1, 2} {
		if lines[enclosed] <= opens || lines[enclosed] >= closes {
			t.Errorf(
				"%s is written on body line %d, outside the guard opened on line %d and "+
					"closed on line %d; outside it the trigger notifies once per row",
				guardedBodyLandmarks[enclosed].name, lines[enclosed], opens, closes,
			)
		}
	}
}

// guardedLandmarkLines locates each landmark and fails unless it occurs exactly once, so the
// enclosure comparison cannot be satisfied by whichever of two copies happened to be first.
func guardedLandmarkLines(t *testing.T, body string) []int {
	t.Helper()
	lines := make([]int, 0, len(guardedBodyLandmarks))
	for _, landmark := range guardedBodyLandmarks {
		line, count := bodyLineHolding(body, landmark.text)
		if count != 1 {
			t.Fatalf(
				"the body writes %s (%q) on %d lines, want exactly one:\n%s",
				landmark.name, landmark.text, count, body,
			)
		}
		lines = append(lines, line)
	}
	return lines
}

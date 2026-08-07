//go:build integration

package schema

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestTheLandingCheckFailsWhenTheQueueRowIsMissing gives criterion 42's functional half its
// falsifiability. The check it drives -- routeRowIssues, the same verdict
// assertGeneratedRouteLandsTheEvent reports -- previously had a guard that read this file's own
// source text and looked for the words "event_queue" and "occurred_at" in it, which a one-line
// comment satisfies. Here the queue row is actually removed and the verdict has to notice.
func TestTheLandingCheckFailsWhenTheQueueRowIsMissing(t *testing.T) {
	skipIfShort(t)

	fixture := aPrivilegeFixture(t)
	writeThroughTheDefinerRoute(t, fixture)

	if issues := routeRowIssues(routeRowsFor(t, fixture.privileged, theRouteMarker)); len(issues) != 0 {
		t.Fatalf("the route did not land before anything was removed, so removing the queue row "+
			"below proves nothing: %v", issues)
	}

	mustExecOn(t, fixture.privileged, theQueueRowRemoval(t))

	issues := routeRowIssues(routeRowsFor(t, fixture.privileged, theRouteMarker))
	if len(issues) != 1 {
		t.Fatalf("with the queue row removed the landing check reported %v, want exactly one "+
			"issue: the event row is still there and only the queue half may complain", issues)
	}
	if !strings.Contains(issues[0], TableEventQueue) {
		t.Errorf("the issue is %q, which does not name the %s row that went missing",
			issues[0], TableEventQueue)
	}
}

// theQueueRowRemoval deletes only the queue row the routed event owns, leaving the event log
// untouched -- which is exactly the state an internal/delivery enqueue that wrote one row and not
// the other would leave behind.
func theQueueRowRemoval(t *testing.T) string {
	t.Helper()

	return "DELETE FROM " + mustQualify(t, harnessSchema, TableEventQueue) + " q USING " +
		mustQualify(t, harnessSchema, TableEvents) + " e" +
		" WHERE q.event_id = e.id AND e.payload->'new'->>'note' = '" + theRouteMarker + "'"
}

// routeRowsQuery counts what one write through the definer route left behind: the event log rows
// carrying the marker, and how many of them a queue row is composed onto by the whole
// (event_id, occurred_at) key. Counting rather than scanning one row is what lets a missing queue
// row be an *answer* here instead of a scan error, so routeRowIssues below can be driven against
// that state by TestTheLandingCheckFailsWhenTheQueueRowIsMissing (routeexec_integration_test.go).
//
// There is deliberately no separate comparison of the two occurred_at values. The queue's composite
// foreign key onto events (id, occurred_at) -- pinned by
// TestTheQueueDeclaresTheRatifiedCompositeKeyAndNoOther -- makes a mismatched pair unrepresentable,
// so such a comparison is a clause no state reachable through this route can falsify.
func routeRowsQuery(t *testing.T) string {
	t.Helper()

	events, queue := mustQualify(t, harnessSchema, TableEvents), mustQualify(t, harnessSchema, TableEventQueue)
	return "SELECT (SELECT count(*) FROM " + events + " e WHERE e.payload->'new'->>'note' = $1)," +
		" (SELECT count(*) FROM " + events + " e JOIN " + queue +
		" q ON q.event_id = e.id AND q.occurred_at = e.occurred_at" +
		" WHERE e.payload->'new'->>'note' = $1)"
}

// routeRows keeps the two counts apart, because the route can fail either way round and one "the
// query found something" answer cannot say which.
type routeRows struct{ events, queuedOnTheCompositeKey int }

func routeRowsFor(t *testing.T, on *pgxpool.Pool, marker string) routeRows {
	t.Helper()

	var got routeRows
	if err := on.QueryRow(t.Context(), routeRowsQuery(t), marker).
		Scan(&got.events, &got.queuedOnTheCompositeKey); err != nil {
		t.Fatalf("count the rows the definer route left behind: %v", err)
	}
	return got
}

// routeRowIssues is the landing verdict as a value rather than as a t.Fatalf, so the control can run
// the same verdict against a state a broken enqueue leaves.
func routeRowIssues(got routeRows) []string {
	var issues []string
	if got.events != 1 {
		issues = append(issues, fmt.Sprintf(
			"%d event log rows carry the route's marker, want exactly 1", got.events))
	}
	if got.queuedOnTheCompositeKey != 1 {
		issues = append(issues, fmt.Sprintf(
			"%d of them are composed onto an %s row by (event_id, occurred_at), want exactly 1",
			got.queuedOnTheCompositeKey, TableEventQueue))
	}
	return issues
}

// assertGeneratedRouteLandsTheEvent checks the generated function's complete durable route: the
// event row and the queue row composed onto it must both exist.
func assertGeneratedRouteLandsTheEvent(t *testing.T, fixture privilegeFixture) {
	t.Helper()

	writeThroughTheDefinerRoute(t, fixture)
	for _, issue := range routeRowIssues(routeRowsFor(t, fixture.privileged, theRouteMarker)) {
		t.Errorf("the write through the definer route did not land: %s", issue)
	}
}

func writeThroughTheDefinerRoute(t *testing.T, fixture privilegeFixture) {
	t.Helper()

	schema, table, _ := strings.Cut(theTriggeredTarget, targetSeparator)
	mustExecOn(t, fixture.application, "INSERT INTO "+mustQualify(t, schema, table)+
		" (id, note) VALUES ($1, $2)", theRouteOrderID, theRouteMarker)
}

package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
)

// The plan-shape clauses internal/schema's committed EXPLAIN depends on, asserted against the
// query issued for a service schema that is *not* the default. claimquerypin_test.go compares the whole text; a
// rendering that reached the queue's name but dropped the row-locking clause on the way is what
// this row is for, and the default schema cannot show it.
func TestClaimQueryHasTheCommittedShape(t *testing.T) {
	queue, err := qualifiedServiceTable(aNonDefaultServiceSchema, schema.TableEventQueue)
	if err != nil {
		t.Fatal(err)
	}
	query, err := claimQueryFor(aNonDefaultServiceSchema)
	if err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{
		"UPDATE " + queue + " q", "FOR UPDATE SKIP LOCKED", "RETURNING q.event_id",
	} {
		if !containsClaimPhrase(query, phrase) {
			t.Errorf("the claim query issued for service schema %s lacks %q", aNonDefaultServiceSchema, phrase)
		}
	}
}

func containsClaimPhrase(query, phrase string) bool {
	return len(query) > len(phrase) && strings.Contains(query, phrase)
}

package schema

import (
	"slices"
	"strings"
	"testing"
)

// AC 39 is ratified -- ADR-9, Accepted, option (a) narrowed -- and this file asserts the shape it
// ratified. The column and the key are asserted separately and deliberately: occurred_at without
// the composite key composes nothing, and the composite primary key on the event log is forced by
// M1 under every option, so neither of those is evidence that the ratified answer was implemented.

// theRatifiedQueueShape is what the queue must declare under the recorded answer, one line each.
var theRatifiedQueueShape = []struct {
	name string
	line string
}{
	{name: "the column the key is composed from",
		line: "    occurred_at timestamptz NOT NULL,"},
	{name: "the composite foreign key itself",
		line: "    FOREIGN KEY (event_id, occurred_at) REFERENCES events (id, occurred_at)"},
}

func TestTheQueueCarriesBothHalvesOfTheRatifiedAnswer(t *testing.T) {
	sql := migrationSQL(t, migrationCreating(t, TableEventQueue))

	for _, half := range theRatifiedQueueShape {
		t.Run(half.name, func(t *testing.T) {
			if !declaresLine(sql, half.line) {
				t.Errorf("the queue does not declare %q, and AC 39's ratified answer makes "+
					"neither half optional", half.line)
			}
		})
	}
}

// theDeclaredForeignKeys is every foreign key the corpus declares, as a closed set. Asserting the
// composite key alone would leave the other two facts unpinned, and both are contract clauses: the
// cascade is what stops internal/reconcile orphaning trigger rows, and deliveries deliberately
// declares none, since a single-column key to the event log is refused outright with "there is no
// unique constraint matching given keys" and composing one would mean carrying occurred_at there
// too.
var theDeclaredForeignKeys = []string{
	"    FOREIGN KEY (listener) REFERENCES listeners (name) ON DELETE CASCADE",
	"    FOREIGN KEY (event_id, occurred_at) REFERENCES events (id, occurred_at)",
}

func TestTheDeclaredForeignKeysAreExactlyThese(t *testing.T) {
	var declared []string
	for _, found := range embeddedCorpusOrFail(t) {
		for _, line := range strings.Split(found.sql, "\n") {
			if strings.HasPrefix(line, "    FOREIGN KEY ") {
				declared = append(declared, strings.TrimSuffix(line, ","))
			}
		}
	}

	if !slices.Equal(declared, theDeclaredForeignKeys) {
		t.Errorf("the corpus declares %q, want exactly %q; deliveries declares none deliberately, "+
			"and a single-column key to the event log is refused by the server",
			declared, theDeclaredForeignKeys)
	}
}

// TestTheDDLRecordsWhyTheQueueCarriesOccurredAt reads back the reason the ratification turned on.
// Without it the column reads as an ordinary denormalisation, and the next author removes it to
// keep the queue narrow -- which is the goal it was knowingly paid against.
func TestTheDDLRecordsWhyTheQueueCarriesOccurredAt(t *testing.T) {
	comments, _ := declaredCommentsIn(migrationSQL(t, migrationCreating(t, TableEventQueue)))
	recorded := comments["COLUMN "+TableEventQueue+".occurred_at"]

	for _, required := range []string{
		"compose the foreign key",
		"no other reason",
		"Phase 5",
	} {
		if !strings.Contains(recorded, required) {
			t.Errorf("the column comment does not record %q; it reads: %q", required, recorded)
		}
	}
	if !strings.Contains(recorded, "server") {
		t.Errorf("the column comment does not record that the refusal to drop a partition holding "+
			"live events is the server's rather than our Go guard's, which is the property the "+
			"decision turned on; it reads: %q", recorded)
	}
}

// TestTheStatusSetIsThreeValuesInACheckAndNotAnEnum pins the whole vocabulary this package declares
// on internal/delivery's behalf. An enum would fight the forward-only one-migration-per-transaction
// rule: ALTER TYPE ... ADD VALUE cannot have its new value referenced until the adding transaction
// commits, which would make the next status change unshippable.
func TestTheStatusSetIsThreeValuesInACheckAndNotAnEnum(t *testing.T) {
	sql := migrationSQL(t, migrationCreating(t, TableEventQueue))

	if !declaresLine(sql, "    status text NOT NULL,") {
		t.Error("status is not declared as text")
	}
	if !declaresLine(sql, "    CHECK (status IN ('pending', 'delivering', 'dead')),") {
		t.Error("the status CHECK does not admit exactly pending, delivering and dead")
	}
	for _, found := range embeddedCorpusOrFail(t) {
		if strings.Contains(found.sql, "CREATE TYPE") {
			t.Errorf("%s creates a type, and the status set is text with a CHECK", found.file)
		}
	}
}

// TestTheCorpusDeclaresExactlyOneCheckConstraint closes the class the row above is one member of.
// The contract names one CHECK; a second one added later -- on response_snippet, say -- would pass
// every assertion written for the first.
func TestTheCorpusDeclaresExactlyOneCheckConstraint(t *testing.T) {
	declared := 0
	for _, found := range embeddedCorpusOrFail(t) {
		declared += linesStartingWith(found.sql, "    CHECK (")
	}

	if declared != 1 {
		t.Errorf("the corpus declares %d CHECK constraints, want only the status one", declared)
	}
}

// TestResponseSnippetIsUnboundedTextWithItsCapAsAComment is the third deliberate irregularity, and
// it looks like a mistake, which is why it is asserted rather than described. A rejected deliveries
// insert would abort internal/delivery's record transaction, leaving the queue row delivering until
// lease reclaim -- turning a truncation bug into an infinite redelivery loop.
func TestResponseSnippetIsUnboundedTextWithItsCapAsAComment(t *testing.T) {
	sql := migrationSQL(t, migrationCreating(t, TableDeliveries))

	if !declaresLine(sql, "    response_snippet text NULL,") {
		t.Error("response_snippet is not declared as unbounded, nullable text")
	}
	comments, _ := declaredCommentsIn(sql)
	recorded := comments["COLUMN "+TableDeliveries+".response_snippet"]
	for _, required := range []string{"2 KiB", "phase 5", "not enforced by a CHECK"} {
		if !strings.Contains(recorded, required) {
			t.Errorf("the column comment does not record %q; it reads: %q", required, recorded)
		}
	}
}

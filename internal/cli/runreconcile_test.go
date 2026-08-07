package cli

import (
	"strings"
	"testing"
)

// TestTheReconcileChoiceRanksTheFlagAboveTheFileAndTheFileAboveTheDefault gives the precedence rows
// where the flag and the file disagree, which is the only kind of row that can tell the documented
// order from its reverse: with every row agreeing, a file-beats-flag implementation answers
// identically and the ordering would be description rather than behaviour.
func TestTheReconcileChoiceRanksTheFlagAboveTheFileAndTheFileAboveTheDefault(t *testing.T) {
	tests := []struct {
		name       string
		choice     reconcileChoice
		configured bool
		want       bool
	}{
		{name: "no flag takes the file's true", configured: true, want: true},
		{name: "no flag takes the file's false", configured: false, want: false},
		{
			name:   "--reconcile beats a file that says false",
			choice: reconcileChoice{reconcile: true, reconcileSet: true}, configured: false, want: true,
		},
		{
			name:   "--reconcile=false beats a file that says true",
			choice: reconcileChoice{reconcile: false, reconcileSet: true}, configured: true, want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.choice.decide(tc.configured); got != tc.want {
				t.Errorf("decide = %t, want %t", got, tc.want)
			}
		})
	}
}

// TestAnAbsentLedgerIsNotCurrentEvenWhenTheVersionsAgree pins the clause that keeps run from serving
// against a database holding none of its tables. Both versions are zero on an absent ledger, so an
// equality-only test of current would answer true and the whole verification would pass vacuously.
func TestAnAbsentLedgerIsNotCurrentEvenWhenTheVersionsAgree(t *testing.T) {
	if (schemaState{applied: 0, expected: 0, present: false}).current() {
		t.Error("an absent ledger reported itself current, so a database with no schema would be served")
	}
	if !(schemaState{applied: 2, expected: 2, present: true}).current() {
		t.Error("a ledger recording every embedded migration reported itself behind")
	}
	if (schemaState{applied: 1, expected: 2, present: true}).current() {
		t.Error("a ledger one migration behind reported itself current")
	}
}

// TestStaleSchemaNamesTheRemedyEachOfItsThreeStatesActuallyHas keeps the three branches apart. A
// database ahead of the binary is the one whose remedy is not bootstrapping, so its row asserts the
// command is absent as well as which advice replaces it: sharing the second branch's wording would
// send an operator to run a command that can only reach a version already passed.
func TestStaleSchemaNamesTheRemedyEachOfItsThreeStatesActuallyHas(t *testing.T) {
	absent := staleSchema("bookings", schemaState{expected: 2}).Error()
	if !strings.Contains(absent, "bookings") || !strings.Contains(absent, "has not been bootstrapped") ||
		!strings.Contains(absent, "`pg_noty bootstrap`") {
		t.Errorf("absent-ledger refusal = %q, want the schema, the state and the command", absent)
	}

	behind := staleSchema("bookings", schemaState{applied: 1, expected: 2, present: true}).Error()
	if !strings.Contains(behind, "version 1") || !strings.Contains(behind, "version 2") ||
		!strings.Contains(behind, "`pg_noty bootstrap`") {
		t.Errorf("behind refusal = %q, want both versions and the command", behind)
	}

	ahead := staleSchema("bookings", schemaState{applied: 3, expected: 2, present: true}).Error()
	if !strings.Contains(ahead, "ahead of the binary") || !strings.Contains(ahead, "roll the binary forward") {
		t.Errorf("ahead refusal = %q, want the direction and its own remedy", ahead)
	}
	if strings.Contains(ahead, "`pg_noty bootstrap`") {
		t.Errorf("ahead refusal = %q, and bootstrapping cannot reach a version the database has passed", ahead)
	}
}

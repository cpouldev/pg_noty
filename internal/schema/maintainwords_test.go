package schema

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// This file is the container-free half of Step 11: the pure functions one maintenance pass decides
// its words and its statement with. They are asserted here rather than through the server because
// none of them touches it, and a rendering asserted through a real create would be asserted
// alongside everything else that create does.
//
// Step 11's Expected Output names two files, one of them production. This step is written across
// ten, for the reason Steps 1 and 3 declared the same deviation: one file of each would breach the
// 200-line budget this package's own gate enforces. The production half is maintain.go (the pass),
// maintainreport.go (the statement it writes and the words it reports in) and maintainrefusal.go
// (what it does about a refusal, split off when the horizon guard landed); the container-backed
// half is maintain_integration_test.go (the fixtures every case is built on), maintainhorizon_
// (AC 25 and the marker), maintainidempotency_ (AC 22 and AC 29), maintainrouting_ (AC 26 to 28),
// maintaindefault_ and maintaingauge_ (AC 30's three halves), maintaincontention_ (AC 33 and AC 40)
// and maintainconcurrency_ (AC 23's precursor and M6).

// theMaintenancePass is this step's production file, named as a constant so the gates other steps
// own join it by name rather than by literal -- the shape Step 10's runner_test.go declared for
// runner.go. It is read by errorelisionscan_test.go's driver-reaching set and by
// catalog_test.go's partition-deciding set.
const theMaintenancePass = "maintain.go"

// theRenderedRange is one range with bounds no formatter can accidentally agree with: a fractional
// second, and a day that is not the epoch's.
var theRenderedRange = Range{
	From: time.Date(2026, time.March, 14, 15, 9, 26, 535000000, time.UTC),
	To:   time.Date(2026, time.March, 15, 15, 9, 26, 535000000, time.UTC),
	Name: "events_probe",
}

// TestTheCreateStatementCarriesNoIfNotExists is M6 asserted at the statement rather than at a
// comment about it. `CREATE TABLE IF NOT EXISTS ... PARTITION OF` matches on the *name*, and a name
// is not an extent: the loser of a true race is told `relation ... already exists` outright
// (measured on 17.10), so nothing about that form can be what makes the concurrent case pass. The
// three-replica precursor in maintaincontention_integration_test.go is the behavioural half, and it
// would still pass with this form present -- which is why the form is refused here by name.
func TestTheCreateStatementCarriesNoIfNotExists(t *testing.T) {
	statement := createPartition(`"noty"."events_probe"`, `"noty"."events"`, theRenderedRange)

	if strings.Contains(strings.ToUpper(statement), "IF NOT EXISTS") {
		t.Errorf("the create is written as %s; that form matches on the name, and a replica that "+
			"agreed on a name while disagreeing on an extent would be told the relation already "+
			"exists and would carry on with the wrong one", statement)
	}
}

// TestTheCreateStatementWritesBothBoundsAsTheInstantsTheyAre pins the two halves of the statement
// that a wrong one would still look right without: the bounds are the arithmetic's own instants at
// full precision, and they are written in UTC. A bound truncated to the second, or rendered in the
// process's zone, names a different range on a sub-second grid and on a machine east of Greenwich
// respectively, and both produce a partition the server accepts.
func TestTheCreateStatementWritesBothBoundsAsTheInstantsTheyAre(t *testing.T) {
	written := createPartition("target", "parent", theRenderedRange)

	for _, wanted := range []string{
		"('" + theRenderedRange.From.Format(time.RFC3339Nano) + "')",
		"('" + theRenderedRange.To.Format(time.RFC3339Nano) + "')",
		"target PARTITION OF parent",
	} {
		if !strings.Contains(written, wanted) {
			t.Errorf("the create is written as %s, which does not carry %s", written, wanted)
		}
	}
}

// TestABoundIsWrittenInUTCWhateverZoneItArrivesIn is the zone half on its own, because the row
// above cannot fail on it: theRenderedRange is already UTC, so a renderer that dropped the
// conversion would agree with it exactly.
func TestABoundIsWrittenInUTCWhateverZoneItArrivesIn(t *testing.T) {
	instant := time.Date(2026, time.March, 14, 15, 9, 26, 535000000, time.UTC)

	if written := boundLiteral(instant.In(time.FixedZone("Kiritimati", 14*60*60))); written !=
		boundLiteral(instant) {
		t.Errorf("the same instant is written %s from one zone and %s from another; a bound is an "+
			"absolute instant and two replicas have to spell it alike", written, boundLiteral(instant))
	}
}

// TestARefusalNamesTheAffectedRangeAsWellAsTheCause is criterion 33's wording. Both halves, because
// a cause with no range is a line an operator cannot act on, and a range with no cause says nothing
// about whether to wait for a lock or to drain the DEFAULT partition.
func TestARefusalNamesTheAffectedRangeAsWellAsTheCause(t *testing.T) {
	refused := creationRefused(theRenderedRange, defaultBlocked(theRenderedRange.Name))

	for _, wanted := range []string{
		boundLiteral(theRenderedRange.From),
		boundLiteral(theRenderedRange.To),
		ErrDefaultBlocked.Error(),
	} {
		if !strings.Contains(refused.Error(), wanted) {
			t.Errorf("the refusal reads %s, which does not name %s", refused.Error(), wanted)
		}
	}
	if !errors.Is(refused, ErrDefaultBlocked) {
		t.Errorf("the refusal no longer matches %v, so a caller cannot tell which condition it "+
			"reports", ErrDefaultBlocked)
	}
}

// TestTheRemedyForABlockedRangeIsTheDrainAndForAnythingElseARetry is ADR-8's operator half, both
// sides. The blocked range does not clear on its own (M4), so its line has to name the command that
// clears it; every other refusal does clear on its own, and naming the drain there would send an
// operator to move rows over a lock that had already been released.
func TestTheRemedyForABlockedRangeIsTheDrainAndForAnythingElseARetry(t *testing.T) {
	blocked := remedyFor(defaultBlocked(theRenderedRange.Name))
	if !strings.Contains(blocked, repairCommand) {
		t.Errorf("a blocked range is answered with %s, which does not name %s -- the one command "+
			"that clears it", blocked, repairCommand)
	}

	timedOut := remedyFor(lockTimedOut("create partition "+theRenderedRange.Name, time.Second))
	if strings.Contains(timedOut, repairCommand) {
		t.Errorf("a lock timeout is answered with %s, which sends an operator to drain rows over a "+
			"lock that has already been released", timedOut)
	}
}

// TestAPassThatWasRefusedARangeIsFailedWhateverElseItCreated is criterion 33's inequality at the
// one function that decides it. The mixed row is the one that matters: a pass that created six
// ranges and was refused the seventh has left a day uncovered, and reporting it as work done is
// exactly the silent failure the criterion exists to forbid.
func TestAPassThatWasRefusedARangeIsFailedWhateverElseItCreated(t *testing.T) {
	for _, tc := range []struct {
		name             string
		created, refused int
		want             Outcome
	}{
		{name: "nothing to do", want: OutcomeNothingNeeded},
		{name: "every range created", created: 8, want: OutcomeWorkDone},
		{name: "one range refused", refused: 1, want: OutcomeFailed},
		{name: "six created and the seventh refused", created: 6, refused: 1, want: OutcomeFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := outcomeOf(tc.created, tc.refused); got != tc.want {
				t.Errorf("a pass that created %d ranges and was refused %d reports %q, want %q",
					tc.created, tc.refused, got, tc.want)
			}
		})
	}
}

// TestAFailedPassIsLoggedAboveRoutineOperation is criterion 33's third clause. The level is
// asserted as the comparison an operator's alerting keys on -- failure strictly above routine --
// rather than against a named constant, so a package that moved routine reporting to debug still
// satisfies the claim that matters.
func TestAFailedPassIsLoggedAboveRoutineOperation(t *testing.T) {
	failed := levelOf(OutcomeFailed)

	for _, routine := range []Outcome{OutcomeNothingNeeded, OutcomeWorkDone} {
		if levelOf(routine) >= failed {
			t.Errorf("a %s pass logs at %s and a failed one at %s, so no alerting rule can tell a "+
				"stalled maintenance loop from a healthy one", routine, levelOf(routine), failed)
		}
	}
	if failed < slog.LevelError {
		t.Errorf("a failed pass logs at %s, below %s; criterion 33 asks for a level an operator can "+
			"key on", failed, slog.LevelError)
	}
}

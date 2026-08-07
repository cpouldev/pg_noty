package schema

import (
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
)

// horizonRow is one required-set case. Every expectation is derived from the epoch grid and the
// derivation is written in the row, never read back from a run. The grid lines are checkable by
// inspection: at 24h they fall on every UTC midnight, at 1h on every UTC hour, and at 168h every
// seven days from 1970-01-01 -- which was a Thursday -- so on every Thursday midnight UTC.
type horizonRow struct {
	name          string
	now           time.Time
	interval      time.Duration
	precreate     time.Duration
	discriminates string
	wantFirstFrom time.Time
	wantLastFrom  time.Time
	wantCount     int
}

var theHorizonRows = []horizonRow{
	{
		// 2026-07-28T06:00 floors to the 07-28 midnight line. The horizon 06:00+48h is
		// 2026-07-30T06:00, which floors to the 07-30 line, so 07-28, 07-29 and 07-30 are
		// required.
		name: "the horizon falls strictly inside a partition",
		now:  utc(2026, 7, 28, 6, 0, 0, 0), interval: 24 * time.Hour, precreate: 48 * time.Hour,
		wantFirstFrom: utc(2026, 7, 28, 0, 0, 0, 0), wantLastFrom: utc(2026, 7, 30, 0, 0, 0, 0),
		wantCount: 3,
	},
	{
		// 06:00+42h is 2026-07-30T00:00:00Z, a grid line itself. TO is exclusive (M11), so an
		// event written at exactly the horizon belongs to the range *starting* there and that
		// range must exist.
		name:          "the horizon falls exactly on an aligned boundary",
		discriminates: "k <= floor(horizon/interval) against k < floor(horizon/interval)",
		now:           utc(2026, 7, 28, 6, 0, 0, 0), interval: 24 * time.Hour, precreate: 42 * time.Hour,
		wantFirstFrom: utc(2026, 7, 28, 0, 0, 0, 0), wantLastFrom: utc(2026, 7, 30, 0, 0, 0, 0),
		wantCount: 3,
	},
	{
		// now sits exactly on a grid line. FROM is inclusive (M11), so the range that owns that
		// instant is the one starting at it, not the one ending at it; and 00:00+24h is the next
		// line exactly, so two ranges are required.
		name:          "now sits exactly on a boundary",
		discriminates: "From <= now against From < now, for which range owns the instant",
		now:           utc(2026, 7, 28, 0, 0, 0, 0), interval: 24 * time.Hour, precreate: 24 * time.Hour,
		wantFirstFrom: utc(2026, 7, 28, 0, 0, 0, 0), wantLastFrom: utc(2026, 7, 29, 0, 0, 0, 0),
		wantCount: 2,
	},
	{
		// 2026-07-28 is a Tuesday; the Thursday line at or before it is 2026-07-23. The horizon
		// 06:00+168h is 2026-08-04T06:00, a Tuesday, whose line is 2026-07-30.
		name: "a 168h interval, whose lines fall on Thursdays",
		now:  utc(2026, 7, 28, 6, 0, 0, 0), interval: 168 * time.Hour, precreate: 168 * time.Hour,
		wantFirstFrom: utc(2026, 7, 23, 0, 0, 0, 0), wantLastFrom: utc(2026, 7, 30, 0, 0, 0, 0),
		wantCount: 2,
	},
	{
		// 06:30 floors to the 06:00 line. The horizon 06:30+90m is 08:00, a line itself, so 06,
		// 07 and 08 are required -- the same discrimination as the daily row, at the interval
		// where a date-only reading of the grid would collapse all three.
		name:          "an hourly interval with the horizon on the hour",
		discriminates: "k <= floor(horizon/interval) against k < floor(horizon/interval)",
		now:           utc(2026, 7, 28, 6, 30, 0, 0), interval: time.Hour, precreate: 90 * time.Minute,
		wantFirstFrom: utc(2026, 7, 28, 6, 0, 0, 0), wantLastFrom: utc(2026, 7, 28, 8, 0, 0, 0),
		wantCount: 3,
	},
}

func TestRequiredRangesSpanTheHorizonInclusiveAtBothEnds(t *testing.T) {
	for _, tc := range theHorizonRows {
		t.Run(
			tc.name, func(t *testing.T) {
				got := RequiredRanges(tc.now, retentionOf(tc.interval, tc.precreate, tc.precreate))

				if len(got) != tc.wantCount {
					t.Fatalf(
						"returned %d ranges %s, want %d (%s)",
						len(got), boundsOf(got), tc.wantCount, tc.discriminates,
					)
				}
				if !got[0].From.Equal(tc.wantFirstFrom) {
					t.Errorf(
						"the first range begins at %s, want the grid line %s",
						got[0].From.UTC().Format(time.RFC3339Nano),
						tc.wantFirstFrom.Format(time.RFC3339Nano),
					)
				}
				if last := got[len(got)-1]; !last.From.Equal(tc.wantLastFrom) {
					t.Errorf(
						"the last range begins at %s, want the grid line %s (%s)",
						last.From.UTC().Format(time.RFC3339Nano),
						tc.wantLastFrom.Format(time.RFC3339Nano), tc.discriminates,
					)
				}
				assertGridShape(t, got, tc.now, tc.interval, tc.precreate)
			},
		)
	}
}

// TestTheRequiredCountIsAConsequenceOfTheKRangeNotALiteral is SC 4. internal/config guarantees
// precreate >= partition_interval (R12) but says nothing about divisibility, so a count pinned as
// precreate/interval + 1 answers two for both instants below and is right for neither.
func TestTheRequiredCountIsAConsequenceOfTheKRangeNotALiteral(t *testing.T) {
	early, late := utc(2026, 7, 28, 6, 0, 0, 0), utc(2026, 7, 28, 20, 0, 0, 0)

	// 30h is not a whole multiple of 24h. early+30h is 2026-07-29T12:00, which floors to the
	// 07-29 line, so 07-28 and 07-29 are required -- two. late+30h is 2026-07-30T02:00, which
	// floors to the 07-30 line, so 07-28, 07-29 and 07-30 are required -- three.
	indivisible := retentionOf(24*time.Hour, 30*time.Hour, 72*time.Hour)
	assertRequiredCount(t, "a non-divisible precreate early in the interval", early, indivisible, 2)
	assertRequiredCount(t, "a non-divisible precreate late in the same interval", late, indivisible, 3)

	// 48h is a whole multiple, so the horizon stays inside the 07-30 partition as now advances
	// through the 07-28 one and the count does not move.
	divisible := retentionOf(24*time.Hour, 48*time.Hour, 72*time.Hour)
	assertRequiredCount(t, "a divisible precreate early in the interval", early, divisible, 3)
	assertRequiredCount(t, "a divisible precreate late in the same interval", late, divisible, 3)
}

func assertRequiredCount(t *testing.T, what string, now time.Time, cfg config.Retention, want int) {
	t.Helper()

	if got := RequiredRanges(now, cfg); len(got) != want {
		t.Errorf("%s requires %d ranges %s, want %d", what, len(got), boundsOf(got), want)
	}
}

// TestNoBoundaryIsAnchoredOnNowOrAnOffsetFromIt is AC 24's second half. now is deliberately off the
// grid -- 06:17:23.5 is no whole multiple of 24h from the epoch -- and precreate and keep are chosen
// so that now+precreate, now-precreate and now-keep are off it too, because a row whose instants
// happen to land on grid lines could not fail. The guard below asserts that premise rather than
// assuming it.
func TestNoBoundaryIsAnchoredOnNowOrAnOffsetFromIt(t *testing.T) {
	now := utc(2026, 7, 28, 6, 17, 23, 500000000)
	cfg := retentionOf(24*time.Hour, 49*time.Hour, 72*time.Hour)

	anchors := map[string]time.Time{
		"now":             now,
		"now + precreate": now.Add(cfg.Precreate),
		"now - precreate": now.Add(-cfg.Precreate),
		"now - keep":      now.Add(-cfg.Keep),
	}
	for label, anchor := range anchors {
		if anchor.UnixNano()%int64(cfg.PartitionInterval) == 0 {
			t.Fatalf("%s is itself a grid line, so no boundary equal to it could be a defect", label)
		}
	}

	for _, ranged := range RequiredRanges(now, cfg) {
		for label, anchor := range anchors {
			assertNotAnchoredOn(t, ranged, anchor, label)
		}
	}
}

func assertNotAnchoredOn(t *testing.T, ranged Range, anchor time.Time, label string) {
	t.Helper()

	if ranged.From.Equal(anchor) || ranged.To.Equal(anchor) {
		t.Errorf(
			"range %s has a bound at %s, so a boundary is anchored on the instant the pass ran "+
				"rather than on the epoch grid; two boots minutes apart would then demand overlapping "+
				"ranges and the server would refuse the second (ADR-6)", boundsOf([]Range{ranged}), label,
		)
	}
}

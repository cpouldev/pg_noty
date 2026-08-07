package schema

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// theAdjacentNameRows are AC 32's collision half. The 24h row passes under a date-only encoding and
// the hourly ones do not: a name carrying only the date produces twenty-four identical identifiers
// per day at partition_interval: 1h, which internal/config permits. The minute and nanosecond rows
// do the same for an hour-resolution and a second-resolution encoding, because internal/config
// requires only that partition_interval be greater than zero (R10).
var theAdjacentNameRows = []struct {
	name     string
	interval time.Duration
	from     time.Time
}{
	{name: "two adjacent 24h ranges", interval: 24 * time.Hour, from: utc(2026, 7, 28, 0, 0, 0, 0)},
	{name: "two adjacent 168h ranges", interval: 168 * time.Hour, from: utc(2026, 7, 23, 0, 0, 0, 0)},
	{name: "the first two 1h ranges of one day", interval: time.Hour, from: utc(2026, 7, 28, 0, 0, 0, 0)},
	{name: "the last two 1h ranges of one day", interval: time.Hour, from: utc(2026, 7, 28, 22, 0, 0, 0)},
	{name: "two adjacent 1m ranges inside one hour", interval: time.Minute, from: utc(2026, 7, 28, 6, 0, 0, 0)},
	{name: "two adjacent 1s ranges inside one minute", interval: time.Second, from: utc(2026, 7, 28, 6, 0, 0, 0)},
	{name: "two adjacent 1ns ranges", interval: time.Nanosecond, from: utc(2026, 7, 28, 6, 0, 0, 0)},
}

func TestAdjacentRangesGetDistinctNames(t *testing.T) {
	for _, tc := range theAdjacentNameRows {
		t.Run(tc.name, func(t *testing.T) {
			middle := tc.from.Add(tc.interval)
			first := rangeName(tc.from, middle)
			second := rangeName(middle, middle.Add(tc.interval))

			if first == second {
				t.Errorf("both ranges are named %q; two partitions cannot share an identifier, and "+
					"the second CREATE would fail with `relation ... already exists` (M6)", first)
			}
			assertWithinTheIdentifierLimit(t, first)
			assertWithinTheIdentifierLimit(t, second)
		})
	}
}

// TestTwoRangesSharingAStartButNotAnEndGetDistinctNames is why the name carries both bounds.
// partition_interval is configuration and can be raised or lowered between boots, so [T, T+1h) and
// [T, T+24h) are distinct ranges the catalog may be asked for in one lifetime; a name derived from
// the start alone would collide them and leave the second permanently uncreatable.
func TestTwoRangesSharingAStartButNotAnEndGetDistinctNames(t *testing.T) {
	from := utc(2026, 7, 28, 0, 0, 0, 0)

	if hourly, daily := rangeName(from, from.Add(time.Hour)),
		rangeName(from, from.Add(24*time.Hour)); hourly == daily {
		t.Errorf("the 1h and the 24h range beginning at %s are both named %q",
			from.Format(time.RFC3339Nano), hourly)
	}
}

// TestTheNameOfAKnownRangeIsWrittenOut pins the encoding itself, so changing it is a decision rather
// than a side effect of some other edit. The name is derived here from the rule, not read back from
// a run: the object being partitioned, then each bound written in UTC as
// YYYYMMDD"T"hhmmss.nnnnnnnnn"Z" -- fixed width, so two bounds differ in their stamp whenever they
// differ at all.
func TestTheNameOfAKnownRangeIsWrittenOut(t *testing.T) {
	got := rangeName(utc(2026, 7, 28, 0, 0, 0, 0), utc(2026, 7, 29, 0, 0, 0, 0))
	want := TableEvents + "_20260728T000000.000000000Z_20260729T000000.000000000Z"

	if got != want {
		t.Errorf("rangeName = %q, want %q", got, want)
	}
	assertWithinTheIdentifierLimit(t, got)
}

// TestARangeNameDoesNotDependOnWhenOrWhereItIsComputed is AC 32's determinism half. Bounds carried
// in another location reach a formatter that omits .UTC(); the ambient swap reaches one that reads
// time.Local. On a UTC machine neither defect is visible without these rows, and both break the
// moment a replica runs elsewhere.
func TestARangeNameDoesNotDependOnWhenOrWhereItIsComputed(t *testing.T) {
	from, to := utc(2026, 7, 28, 0, 0, 0, 0), utc(2026, 7, 29, 0, 0, 0, 0)
	inUTC := rangeName(from, to)

	for _, tc := range []struct {
		name string
		zone *time.Location
	}{
		{name: "computed a second time in one process", zone: time.UTC},
		{name: "the same instants carried in and run under a +14 zone", zone: aheadOfUTC},
		{name: "the same instants carried in and run under a -11 zone", zone: behindUTC},
	} {
		t.Run(tc.name, func(t *testing.T) {
			underLocalZone(t, tc.zone, func() {
				if got := rangeName(from.In(tc.zone), to.In(tc.zone)); got != inUTC {
					t.Errorf("rangeName = %q, want the byte-identical %q the UTC evaluation gave",
						got, inUTC)
				}
			})
		})
	}
}

// theIdentifierLengthRows are SC 8 and CK-10. `full` is base + "_" + suffix, so a row's length is
// len(base) + 1 + len(suffix): 2 + 1 + 59 = 62, 2 + 1 + 60 = 63 and 2 + 1 + 61 = 64. The 63-byte row
// is the equality case, and it is the only input that tells `len(full) <= 63` from `len(full) < 63`.
var theIdentifierLengthRows = []struct {
	name, discriminates string
	suffixBytes         int
	wantWhole           bool
}{
	{name: "a name one byte under the limit", suffixBytes: 59, wantWhole: true},
	{
		name: "a name of exactly the 63-byte limit", suffixBytes: 60, wantWhole: true,
		discriminates: "len(full) <= 63 against len(full) < 63",
	},
	{
		name: "a name one byte over the limit", suffixBytes: 61, wantWhole: false,
		discriminates: "len(full) <= 63 against len(full) <= 64",
	},
}

func TestAnIdentifierIsWholeUpToTheLimitAndHashedPastIt(t *testing.T) {
	const base = "ab"
	for _, tc := range theIdentifierLengthRows {
		t.Run(tc.name, func(t *testing.T) {
			full := base + "_" + strings.Repeat("s", tc.suffixBytes)
			got := ObjectName(base, strings.Repeat("s", tc.suffixBytes))

			assertWithinTheIdentifierLimit(t, got)
			if tc.wantWhole && got != full {
				t.Errorf("ObjectName = %q (%d bytes), want the whole %d-byte name %q (%s)",
					got, len(got), len(full), full, tc.discriminates)
			}
			if !tc.wantWhole && (got == full || got == full[:MaxIdentifierBytes]) {
				t.Errorf("ObjectName = %q, which is the %d-byte name whole or plainly truncated; "+
					"truncating alone is what collides two names (M9, %s)",
					got, len(full), tc.discriminates)
			}
		})
	}
}

// TestTwoNamesAgreeingOnTheirFirst63BytesAreStillDistinct is M9's own case. Postgres truncates a
// longer identifier silently, with only a NOTICE, so under a plain truncation both inputs below
// become one identifier and the second CREATE fails with `relation ... already exists`.
func TestTwoNamesAgreeingOnTheirFirst63BytesAreStillDistinct(t *testing.T) {
	const base = TableEvents
	shared := strings.Repeat("a", 60)
	firstFull, secondFull := base+"_"+shared+"X", base+"_"+shared+"Y"

	if firstFull[:MaxIdentifierBytes] != secondFull[:MaxIdentifierBytes] {
		t.Fatalf("%q and %q do not agree on their first %d bytes, so this pair could not collide "+
			"under truncation and the row could not fail",
			firstFull, secondFull, MaxIdentifierBytes)
	}

	first, second := ObjectName(base, shared+"X"), ObjectName(base, shared+"Y")
	if first == second {
		t.Errorf("both %d-byte names resolve to %q; the hash is taken over the truncated name "+
			"rather than the whole one", len(firstFull), first)
	}
	assertWithinTheIdentifierLimit(t, first)
	assertWithinTheIdentifierLimit(t, second)
}

// TestAnOverLongNameIsCutOnARuneBoundary reaches the guard directly, because every caller in this
// package writes ASCII and so no range can reach it. A raw byte cut here would leave half a UTF-8
// sequence in an identifier.
func TestAnOverLongNameIsCutOnARuneBoundary(t *testing.T) {
	base := "x" + strings.Repeat("é", 40)
	full := base + "_" + "y"
	keep := MaxIdentifierBytes - identifierHashHexDigits - 1

	if utf8.RuneStart(full[keep]) {
		t.Fatalf("byte %d of %q begins a rune, so a raw byte cut would produce valid UTF-8 anyway "+
			"and this row could not fail", keep, full)
	}

	got := ObjectName(base, "y")
	assertWithinTheIdentifierLimit(t, got)
	if !utf8.ValidString(got) {
		t.Errorf("ObjectName = %q, which is not valid UTF-8", got)
	}
}

// assertWithinTheIdentifierLimit is the invariant every name in this corpus shares: PostgreSQL
// stores at most NAMEDATALEN-1 bytes of an identifier and silently drops the rest.
func assertWithinTheIdentifierLimit(t *testing.T, name string) {
	t.Helper()

	if len(name) > MaxIdentifierBytes {
		t.Errorf("%q is %d bytes, over PostgreSQL's %d-byte identifier limit; it would be truncated "+
			"with only a NOTICE (M9)", name, len(name), MaxIdentifierBytes)
	}
}

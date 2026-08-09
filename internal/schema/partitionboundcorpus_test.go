package schema

import "time"

// recordedBound is one bound expression the reader has to answer for. Every written value below is
// server output, recorded from a live catalog, and
// TestTheServerStillRendersEveryShapeTheReaderWasWrittenFor asks PostgreSQL to write each of them
// again -- so this corpus takes its authority from the thing it stands in for rather than from the
// reader it is used to test.
type recordedBound struct {
	name, written string
	// from and to are counted off the written text -- the offset subtracted by hand, the fraction
	// read digit by digit -- rather than taken from a run of the reader. They are meaningful only
	// where fault is boundOK.
	from, to time.Time
	fault    boundFault
	// isDefault marks the one row isDefaultBound accepts.
	isDefault bool
	// parentColumns, boundClause and session are how the server is asked to render this row again:
	// a parent of its own, one partition, and the settings the rendering was measured under. An
	// empty boundClause means no partition can produce this text at all, which the two rows carrying
	// one say for themselves.
	parentColumns, boundClause string
	session                    []string
}

const (
	instantParent  = "(occurred_at timestamptz) PARTITION BY RANGE (occurred_at)"
	naiveParent    = "(a timestamp) PARTITION BY RANGE (a)"
	twoPartParent  = "(a timestamptz, b int) PARTITION BY RANGE (a, b)"
	labelledParent = "(k text) PARTITION BY LIST (k)"
	spreadParent   = "(k int) PARTITION BY HASH (k)"

	// oneDayIn2026 is the extent every row that varies only by session setting is written over, so
	// those rows differ in the rendering and in nothing else.
	oneDayIn2026 = "FOR VALUES FROM ('2026-01-01T00:00:00Z') TO ('2026-01-02T00:00:00Z')"
)

// utcInstant is one expected bound: the instant the written text names once its offset has been
// applied. Nanoseconds, because a bound carries microseconds at most.
func utcInstant(year int, month time.Month, day, hour, minute, second, nanosecond int) time.Time {
	return time.Date(year, month, day, hour, minute, second, nanosecond, time.UTC)
}

var (
	januaryFirst  = utcInstant(2026, time.January, 1, 0, 0, 0, 0)
	januarySecond = utcInstant(2026, time.January, 2, 0, 0, 0, 0)
)

var recordedBounds = []recordedBound{
	{name: "a single-column range under the default session", fault: boundOK,
		written: "FOR VALUES FROM ('2026-01-01 00:00:00+00') TO ('2026-01-02 00:00:00+00')",
		from:    januaryFirst, to: januarySecond,
		parentColumns: instantParent, boundClause: oneDayIn2026},

	// .123456 is 123456 microseconds, which is 123456000 nanoseconds.
	{name: "a range carrying microseconds", fault: boundOK,
		written: "FOR VALUES FROM ('2026-06-01 00:00:00.123456+00') TO " +
			"('2026-06-01 00:00:00.123457+00')",
		from:          utcInstant(2026, time.June, 1, 0, 0, 0, 123456000),
		to:            utcInstant(2026, time.June, 1, 0, 0, 0, 123457000),
		parentColumns: instantParent,
		boundClause: "FOR VALUES FROM ('2026-06-01T00:00:00.123456Z') " +
			"TO ('2026-06-01T00:00:00.123457Z')"},

	// The next three are the first row's own extent re-rendered under another TimeZone, so each
	// names the same two instants and a reader that dropped the offset answers the local reading
	// instead. 19:00 at -05 is 00:00 the next day at UTC; 05:30 at +05:30 and 05:45 at +05:45 are
	// both 00:00 on the day they are written on.
	{name: "an hour-wide offset", fault: boundOK,
		written: "FOR VALUES FROM ('2025-12-31 19:00:00-05') TO ('2026-01-01 19:00:00-05')",
		from:    januaryFirst, to: januarySecond,
		parentColumns: instantParent, boundClause: oneDayIn2026,
		session: []string{"SET TimeZone TO 'America/New_York'"}},
	{name: "a half-hour offset", fault: boundOK,
		written: "FOR VALUES FROM ('2026-01-01 05:30:00+05:30') TO ('2026-01-02 05:30:00+05:30')",
		from:    januaryFirst, to: januarySecond,
		parentColumns: instantParent, boundClause: oneDayIn2026,
		session: []string{"SET TimeZone TO 'Asia/Kolkata'"}},
	{name: "a three-quarter-hour offset", fault: boundOK,
		written: "FOR VALUES FROM ('2026-01-01 05:45:00+05:45') TO ('2026-01-02 05:45:00+05:45')",
		from:    januaryFirst, to: januarySecond,
		parentColumns: instantParent, boundClause: oneDayIn2026,
		session: []string{"SET TimeZone TO 'Asia/Kathmandu'"}},
	// A local mean time zone puts seconds in the offset: 00:17:30 at +00:17:30 is 00:00:00 UTC.
	{name: "an offset carrying seconds", fault: boundOK,
		written: "FOR VALUES FROM ('1850-01-01 00:17:30+00:17:30') TO " +
			"('1860-01-01 00:17:30+00:17:30')",
		from:          utcInstant(1850, time.January, 1, 0, 0, 0, 0),
		to:            utcInstant(1860, time.January, 1, 0, 0, 0, 0),
		parentColumns: instantParent,
		boundClause:   "FOR VALUES FROM ('1850-01-01T00:00:00Z') TO ('1860-01-01T00:00:00Z')",
		session:       []string{"SET TimeZone TO 'Europe/Amsterdam'"}},

	{name: "the DEFAULT partition", written: defaultBoundExpression, isDefault: true,
		fault:         boundNotASingleColumnRange,
		parentColumns: instantParent, boundClause: "DEFAULT"},

	{name: "a range unbounded above", fault: boundHasNoFiniteEnd,
		written:       "FOR VALUES FROM ('2027-01-02 00:00:00+00') TO (MAXVALUE)",
		parentColumns: instantParent,
		boundClause:   "FOR VALUES FROM ('2027-01-02T00:00:00Z') TO (MAXVALUE)"},
	{name: "a range unbounded below", fault: boundHasNoFiniteEnd,
		written:       "FOR VALUES FROM (MINVALUE) TO ('2020-01-01 00:00:00+00')",
		parentColumns: instantParent,
		boundClause:   "FOR VALUES FROM (MINVALUE) TO ('2020-01-01T00:00:00Z')"},

	{name: "a two-column range", fault: boundNotASingleColumnRange,
		written:       "FOR VALUES FROM ('2026-01-01 00:00:00+00', 0) TO ('2026-01-02 00:00:00+00', 0)",
		parentColumns: twoPartParent,
		boundClause:   "FOR VALUES FROM ('2026-01-01T00:00:00Z', 0) TO ('2026-01-02T00:00:00Z', 0)"},
	{name: "a list partition", fault: boundNotASingleColumnRange,
		written: "FOR VALUES IN ('a')", parentColumns: labelledParent,
		boundClause: "FOR VALUES IN ('a')"},
	{name: "a list partition whose value holds a quote", fault: boundNotASingleColumnRange,
		written: "FOR VALUES IN ('has ''quote''')", parentColumns: labelledParent,
		boundClause: "FOR VALUES IN ('has ''quote''')"},
	{name: "a hash partition", fault: boundNotASingleColumnRange,
		written: "FOR VALUES WITH (modulus 2, remainder 0)", parentColumns: spreadParent,
		boundClause: "FOR VALUES WITH (MODULUS 2, REMAINDER 0)"},

	{name: "a range over a column carrying no zone", fault: boundInstantUnreadable,
		written:       "FOR VALUES FROM ('2026-01-01 00:00:00') TO ('2026-01-02 00:00:00')",
		parentColumns: naiveParent,
		boundClause:   "FOR VALUES FROM ('2026-01-01T00:00:00') TO ('2026-01-02T00:00:00')"},
	{name: "a range rendered under another DateStyle", fault: boundInstantUnreadable,
		written:       "FOR VALUES FROM ('31/12/2025 19:00:00 EST') TO ('01/01/2026 19:00:00 EST')",
		parentColumns: instantParent, boundClause: oneDayIn2026,
		session: []string{"SET TimeZone TO 'America/New_York'", "SET DateStyle TO 'SQL, DMY'"}},

	// The last two carry no boundClause because no partition renders them, and that is exactly why
	// they are here: the refusal must not be gated on the bound string looking plausible, being
	// non-empty, or carrying any marker substring. The second holds the opening marker verbatim
	// and nothing else the reader needs.
	//
	// The first was once described here as what a NULL bound coalesced to, and it is not one any
	// more: the observation carries the NULL through as its own answer rather than flattening it to
	// this string, because a partition dropped mid-query and a bound that cannot be read are
	// different facts (cataloginventory.go). This row is now only what it says it is -- an empty
	// rendering, which nothing produces and the reader must still refuse.
	{name: "nothing at all, which no partition renders",
		written: "", fault: boundNotASingleColumnRange},
	{name: "the opening marker with no closing one",
		written: "FOR VALUES FROM ('2026-01-01 00:00:00+00')", fault: boundNotASingleColumnRange},
}

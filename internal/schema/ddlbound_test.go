package schema

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// This file is the bound itself, before any server sees it: how a Go duration becomes the number
// PostgreSQL counts lock_timeout in, and what the statement carrying it looks like.
//
// It is container-free because the conversion is arithmetic. The behavioural half -- that a
// transaction running under that statement really does give up, and really does see the bound this
// package configured -- is ddl_integration_test.go's, and neither cites the other.

// TestTheBoundIsCountedInTheUnitPostgresCountsItIn pins the unit against the server's own, measured
// on 17.10: `SET LOCAL lock_timeout = 1000` renders as `1s` under SHOW and as `1000` with `unit =
// ms` in pg_settings. A bound written in seconds would be a thousand times the one intended, and
// every timeout row would still pass -- slowly.
func TestTheBoundIsCountedInTheUnitPostgresCountsItIn(t *testing.T) {
	if got, want := millisecondsOfBound(DefaultLockTimeout), int64(3000); got != want {
		t.Errorf("the %s default renders as %d, want %d; lock_timeout counts milliseconds",
			DefaultLockTimeout, got, want)
	}
}

// TestASubMillisecondBoundIsNeverRoundedDownToNoBoundAtAll is the fail-open direction, and the only
// one that matters here: zero does not mean "give up at once" to PostgreSQL, it means "wait
// forever". A bound that rounded down would turn the availability control M5 makes this into an
// unbounded stall on the customer's writes, and it would do so silently.
//
// The exactly-one-millisecond row is what separates the guard from its neighbours: every row above
// and below it passes under either `> 0` or `>= 0`.
func TestASubMillisecondBoundIsNeverRoundedDownToNoBoundAtAll(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bound time.Duration
		want  int64
		why   string
	}{
		{name: "two milliseconds", bound: 2 * time.Millisecond, want: 2,
			why: "a bound the unit expresses exactly is carried across unchanged"},
		{name: "exactly one millisecond", bound: time.Millisecond, want: 1,
			why: "the smallest bound the unit expresses is a bound, not an absence of one"},
		{name: "just under a millisecond", bound: 999 * time.Microsecond, want: 1,
			why: "truncation would write 0, which PostgreSQL reads as no timeout at all"},
		{name: "no bound at all", bound: 0, want: 1,
			why: "normalized replaces a zero before this is reached, so reaching it is a caller " +
				"error -- and the shortest bound is the safe answer to one"},
		{name: "a negative bound", bound: -time.Second, want: 1,
			why: "the server refuses a negative setting outright, and refusing to run is worse " +
				"than running under the tightest bound there is"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := millisecondsOfBound(tc.bound); got != tc.want {
				t.Errorf("millisecondsOfBound(%s) = %d, want %d: %s", tc.bound, got, tc.want, tc.why)
			}
		})
	}
}

// TestTheBoundIsWrittenAsASetLocal is what keeps the bound inside its own transaction. A pooled
// connection outlives every statement that ran on it, so a session-level SET would bound the next
// caller's unrelated work at whatever this pass happened to configure -- and would keep doing so
// until that connection was reaped.
func TestTheBoundIsWrittenAsASetLocal(t *testing.T) {
	written := lockTimeoutStatement(DefaultLockTimeout)

	if !strings.HasPrefix(written, "SET LOCAL ") {
		t.Errorf("the bound is written as %q, which is not a SET LOCAL", written)
	}
	// The number is taken from the conversion rather than written again, so this row asserts the
	// rendering and the row above asserts the unit -- one claim each.
	want := strconv.FormatInt(millisecondsOfBound(DefaultLockTimeout), 10)
	if !strings.HasSuffix(written, want) {
		t.Errorf("the bound is written as %q, which does not end in the %s milliseconds the %s "+
			"default counts to", written, want, DefaultLockTimeout)
	}
}

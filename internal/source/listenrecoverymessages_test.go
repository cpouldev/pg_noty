package source

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// The documented reporting path, named, and the retry ladder's own arithmetic.
//
// The contract requires the loss to be surfaced once through the source's reporting path rather
// than silently, so the integration cases count messages. listen.go writes those messages inline at
// its reportListen call sites, and a rename there would leave the counting looking satisfied while
// counting a message nothing emits; TestTheReportedListenMessagesAreTheOnesListenGoWrites is what
// makes the rename fail here instead.

const (
	// theListenUnavailableMessage is reported once per run of failed connection attempts (listen.go's
	// connect-failure latch).
	theListenUnavailableMessage = "listen unavailable"
	// theListenConnectedMessage is reported on every successful connection, and is what resets both
	// latches.
	theListenConnectedMessage = "listen connected"
	// theListenLostMessage is reported once per lost connection (listen.go's receive-failure latch).
	theListenLostMessage = "listen connection lost"
)

var reportedListenMessages = []string{
	theListenUnavailableMessage, theListenConnectedMessage, theListenLostMessage,
}

// attemptsWithin is how many connection attempts fit inside a window under the ladder the constants
// declare: one immediately, then one after each successive delay, the delay doubling from
// listenInitialBackoff and capped at listenMaximumBackoff.
//
// The doubling is written out here rather than taken from nextListenBackoff, and deliberately. A
// bound computed by the mechanism under test moves with it: measured, a nextListenBackoff that
// stopped doubling raised this bound from 6 attempts per second to 51 and its own spin passed.
// The two are held together by TestTheLadderTheLoopUsesIsTheOneTheBoundAssumes rather than by
// sharing a body.
func attemptsWithin(window time.Duration) int {
	elapsed, delay, attempts := time.Duration(0), listenInitialBackoff, 1
	for {
		elapsed += delay
		if elapsed > window {
			return attempts
		}
		attempts, delay = attempts+1, min(delay*2, listenMaximumBackoff)
	}
}

func TestTheReportedListenMessagesAreTheOnesListenGoWrites(t *testing.T) {
	written := string(sourceBytes(t, "listen.go"))
	for _, message := range reportedListenMessages {
		if !strings.Contains(written, strconv.Quote(message)) {
			t.Errorf("listen.go writes no %s, so the cases counting it count a message nothing "+
				"emits", strconv.Quote(message))
		}
	}
	if len(reportedListenMessages) != 3 {
		t.Fatalf("%d reported messages; listen.go's reportListen has three call sites",
			len(reportedListenMessages))
	}
}

// TestTheLadderTheLoopUsesIsTheOneTheBoundAssumes keeps the two independent expressions of the
// ladder from drifting apart. The bound above is derived from the constants so that a mechanism
// which stopped growing fails the window count; this is where a mechanism the constants do not
// explain fails by name instead of by an arithmetic surprise elsewhere.
func TestTheLadderTheLoopUsesIsTheOneTheBoundAssumes(t *testing.T) {
	delay := listenInitialBackoff
	for rung := range 12 {
		want := min(delay*2, listenMaximumBackoff)
		got := nextListenBackoff(delay)
		if got != want {
			t.Fatalf("rung %d: nextListenBackoff(%s) = %s, want the doubling capped at %s that the "+
				"attempt bound is derived from", rung, delay, got, listenMaximumBackoff)
		}
		delay = got
	}
}

func TestTheAttemptBoundGrowsWithTheLadderRatherThanWithTheWindow(t *testing.T) {
	window := 20 * listenInitialBackoff
	first, doubled := attemptsWithin(window), attemptsWithin(2*window)
	// A ladder that never delayed would fit window/listenInitialBackoff attempts into the first
	// window and twice as many into the second; a growing one cannot.
	if doubled >= 2*first {
		t.Errorf("the bound allows %d attempts in %s and %d in %s, which is what a fixed retry "+
			"would allow: this bound cannot separate a delay from a spin", first, window, doubled, 2*window)
	}
	if first < 2 {
		t.Errorf("the bound allows %d attempts in %s, so a source that never retried would satisfy it",
			first, window)
	}
}

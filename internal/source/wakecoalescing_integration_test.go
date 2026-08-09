//go:build integration

package source

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The wake-up hint's own case, which nothing reached: with the channel unread, several transactions
// commit in succession, the producing loop neither blocks nor dies, and a later read still receives
// a wake-up. listen_test.go's TestWakeSignalIsCapOneAndNonBlocking is the unit half; it calls
// signalWake against a hand-built channel, so listen.go's producing loop could be deleted outright
// and it would still pass. The wake-ups here come from committed transactions firing a generated
// trigger, and nothing reads the channel while they arrive.
//
// The two facts are asserted at different moments, because only one of them can be
// observed while the loop is running. Coalescing is counted after Close has returned: Close returns
// only once the producing goroutine has, so nothing further can be sent and what the channel holds
// is final, where a count taken mid-run cannot tell a coalescing channel from an accumulating one a
// fast reader happened to keep drained. "Never blocks" is measured by leaving the cap-1 channel full
// while further transactions commit and then requiring Close to return -- a send that blocked would
// park the listening goroutine on it, and cancelling a context cannot interrupt a bare channel send,
// so listenerDone would never close.
const (
	// theCoalescingTable is the trigger-bearing table whose commits produce the burst, and
	// theCoalescingListener names the generated listener installed on it.
	theCoalescingTable    = "coalescing_target"
	theCoalescingListener = "coalescing"
	// theUnreadCommitCount is how many transactions commit with nobody reading. It is well above the
	// channel's capacity of one, so an accumulating channel shows as a backlog rather than as a
	// timing accident.
	theUnreadCommitCount = 8
	// theFullChannelCommitCount is how many commit while the channel already holds an unread wake-up.
	// The first fills it and every one after it meets a full buffer, which is the state a blocking
	// send parks in.
	theFullChannelCommitCount = 4
	// theBurstDispatchDeadline is how long the server is given to hand one notification per commit to
	// a listening connection of the test's own.
	theBurstDispatchDeadline = 5 * time.Second
	// theNotificationSettlingWindow is how long the source's own backend is then given to drain what
	// the server has already handed to that connection. They are different backends and the server
	// promises no ordering between them, so this is what makes "nothing more is readable" a statement
	// about coalescing rather than about a notification still in flight.
	theNotificationSettlingWindow = 500 * time.Millisecond
)

func TestUnreadWakeUpsCoalesceAndNeverBlockTheProducingLoop(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, "CREATE TABLE public."+theCoalescingTable+" (id int)")
	installInsertTrigger(t, pool, "noty", theCoalescingListener, theCoalescingTable)
	source := openListeningSource(t, pool, harnessConfig(t), newListenLog())
	freeAParkedProducerBeforeTheDeferredClose(t, source)
	waitForDedicatedListener(t, pool, source)
	drainWakes(t, source)

	// Opened after the warm-up, so it counts this burst's notifications and no earlier probe's.
	control := openNotificationConnection(t, harnessConfig(t), "noty")
	commitTriggeringInserts(t, pool, control, theUnreadCommitCount)

	assertALaterReadStillReceivesAWakeUp(t, source)
	assertAFurtherCommitStillWakesTheReader(t, pool, control, source)

	// Nothing reads from here on, so the first of these fills the channel and the rest meet it full.
	commitTriggeringInserts(t, pool, control, theFullChannelCommitCount)
	assertCloseReturnsAndOneCoalescedWakeSurvived(t, source)
}

// freeAParkedProducerBeforeTheDeferredClose keeps a failure here reportable. This case leaves the
// cap-1 channel holding an unread wake-up throughout, so under the very defect it exists to find --
// a send that blocks -- the listening goroutine is parked on it and openListeningSource's deferred
// Close waits for that goroutine forever, turning the named failure into a package timeout charged
// to whichever case the deadline happens to kill. Cleanups run last-in first-out and this one is
// registered after that Close, so it drains ahead of it.
func freeAParkedProducerBeforeTheDeferredClose(t *testing.T, source *TriggerSource) {
	t.Helper()
	t.Cleanup(func() {
		go func() {
			for {
				if _, open := <-source.Notify(); !open {
					return
				}
			}
		}()
	})
}

// commitTriggeringInserts commits count transactions against the trigger-bearing table and waits
// until the server has handed one notification per commit to a listening connection of the test's
// own. Without that confirmation every assertion below passes vacuously against a trigger that
// stopped firing: "no second wake-up is readable" is exactly what a source with nothing to deliver
// answers. The settle that follows is for the source's own backend, which is a different backend and
// is promised no ordering with this one.
func commitTriggeringInserts(t *testing.T, pool *pgxpool.Pool, control *pgx.Conn, count int) {
	t.Helper()
	for id := 1; id <= count; id++ {
		mustExecOn(t, pool, "INSERT INTO public."+theCoalescingTable+" VALUES ($1)", id)
	}
	for delivered := 0; delivered < count; delivered++ {
		if !hasNotification(t, control, theBurstDispatchDeadline) {
			t.Fatalf("the server delivered %d notifications for %d committed transactions within %s, "+
				"so the burst these assertions are about never happened", delivered, count,
				theBurstDispatchDeadline)
		}
	}
	time.Sleep(theNotificationSettlingWindow)
}

// assertALaterReadStillReceivesAWakeUp is the wake-up contract's own clause -- a reader that
// finally reads still gets its wake-up -- plus the bound that makes the hint a signal rather than a queue. Every
// notification of the burst has already been delivered, so one read empties the channel whatever
// the burst's size, and a channel that accumulated would still hold theUnreadCommitCount-1 of them.
func assertALaterReadStillReceivesAWakeUp(t *testing.T, source *TriggerSource) {
	t.Helper()
	select {
	case _, open := <-source.Notify():
		if !open {
			t.Fatal("the wake-up channel is closed while the source is running; Close alone closes it")
		}
	case <-time.After(theBurstDispatchDeadline):
		t.Fatalf("no wake-up was readable after %d committed transactions, so an unread backlog "+
			"stalls the reader it was produced for", theUnreadCommitCount)
	}
	// Recorded rather than fatal, so a run reaching here also reaches the Close deadline below: an
	// accumulating channel fails only this clause, a send that blocks fails both (the parked sender's
	// value lands in the buffer as this read empties it), and the pair tells the two defects apart.
	select {
	case <-source.Notify():
		t.Errorf("a second wake-up was readable immediately after the first: %d unread notifications "+
			"accumulated instead of coalescing into one repeating hint", theUnreadCommitCount)
	default:
	}
}

// assertAFurtherCommitStillWakesTheReader is the liveness half: the producing loop survived the
// unread backlog rather than parking on a send or dying under it.
func assertAFurtherCommitStillWakesTheReader(t *testing.T, pool *pgxpool.Pool, control *pgx.Conn, source *TriggerSource) {
	t.Helper()
	commitTriggeringInserts(t, pool, control, 1)
	select {
	case _, open := <-source.Notify():
		if !open {
			t.Fatal("the wake-up channel is closed while the source is running; Close alone closes it")
		}
	case <-time.After(theBurstDispatchDeadline):
		t.Fatalf("a transaction committed after %d unread ones woke nobody, so the producing loop did "+
			"not survive the backlog", theUnreadCommitCount)
	}
}

// assertCloseReturnsAndOneCoalescedWakeSurvived carries both remaining clauses.
// The channel has been full since the first commit of the last burst, so a blocking send parks the
// listening goroutine on it and Close -- which waits for that goroutine -- never returns; Close is
// driven off this goroutine so that failure is a named deadline rather than a hang charged to
// whichever case the package timeout happens to kill. Once Close has returned the goroutine has
// returned too, so the channel's contents are final and counting them is a race-free measurement of
// coalescing.
func assertCloseReturnsAndOneCoalescedWakeSurvived(t *testing.T, source *TriggerSource) {
	t.Helper()
	returned := make(chan error, 1)
	go func() { returned <- source.Close() }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatalf("Close returned %v with wake-ups unread, want nil", err)
		}
	case <-time.After(theCloseDeadline):
		t.Fatalf("Close did not return within %s while %d notifications arrived into a full wake-up "+
			"channel: the producing loop is parked on a send that no cancellation can interrupt",
			theCloseDeadline, theFullChannelCommitCount)
	}

	held := 0
	for held <= theFullChannelCommitCount {
		select {
		case _, open := <-source.Notify():
			if !open {
				if held != 1 {
					t.Errorf("the wake-up channel held %d wake-ups when the producing loop returned, "+
						"want the single coalesced one: 0 means no committed transaction ever woke the "+
						"source, more than 1 means %d unread notifications accumulated", held,
						theFullChannelCommitCount)
				}
				return
			}
			held++
		case <-time.After(time.Second):
			t.Fatal("the wake-up channel is still open after Close returned, so a consumer selecting " +
				"on it never learns the source has finished")
		}
	}
	t.Fatalf("the wake-up channel held at least %d wake-ups when the producing loop returned: %d "+
		"unread notifications accumulated rather than coalescing", held, theFullChannelCommitCount)
}

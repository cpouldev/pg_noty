package delivery

import (
	"context"
	"testing"
	"time"
)

func TestWakeMachinePollIsUnconditionalThroughSilentAndClosedNotify(t *testing.T) {
	machine := newWakeMachine(time.Second, func(int64) int64 { return 0 })
	if !machine.poll().poll {
		t.Fatal("silent listener did not produce the poll action")
	}
	machine.notify(false)
	if !machine.poll().poll {
		t.Fatal("closed listener stopped the unconditional poll action")
	}
	if machine.listening {
		t.Fatal("closed listener remained marked as listening")
	}
}

func TestWakeMachineReconnectSequenceDoesNotChangePollGuarantee(t *testing.T) {
	machine := newWakeMachine(time.Second, nil)
	if !machine.lost() {
		t.Fatal("first listener loss did not request reconnect")
	}
	if machine.lost() {
		t.Fatal("repeated loss started a second reconnect")
	}
	machine.reconnected(true)
	if !machine.listening || machine.reconnecting {
		t.Fatalf("reconnected state = listening:%v reconnecting:%v", machine.listening, machine.reconnecting)
	}
	if !machine.poll().poll {
		t.Fatal("poll action disappeared after reconnect")
	}
	machine.reconnected(false)
	if machine.listening {
		t.Fatal("failed reconnect reported an open listener")
	}
}

func TestWakeLoopReconnectRunsBehindThePollSelect(t *testing.T) {
	listener := make(chan struct{})
	called := 0
	loop := newWakeLoop(time.Second, nil, func(int64) int64 { return 0 }, func(context.Context) (<-chan struct{}, error) {
		called++
		return listener, nil
	})
	result := loop.startReconnect(context.Background())
	if got := <-result; got != listener {
		t.Fatal("reconnect seam returned the wrong listener channel")
	}
	if called != 1 {
		t.Fatalf("reconnect calls = %d, want one", called)
	}
}

func TestWakeDelayIsBoundedAndUsesInjectedNonIdenticalDraws(t *testing.T) {
	interval := 100 * time.Millisecond
	draws := []int64{3, 77}
	draw := func(max int64) int64 {
		if max != int64(interval)+1 {
			t.Fatalf("jitter bound = %d, want %d", max, int64(interval)+1)
		}
		value := draws[0]
		draws = draws[1:]
		return value
	}
	first, second := wakeDelay(interval, draw), wakeDelay(interval, draw)
	if first < 0 || first > interval || second < 0 || second > interval {
		t.Fatalf("wake delays %s and %s escaped [0,%s]", first, second, interval)
	}
	if first == second {
		t.Fatalf("injected wake draws collapsed to one delay: %s", first)
	}
}

func TestWakeDelayClampsHostileDraw(t *testing.T) {
	if got := wakeDelay(time.Second, func(int64) int64 { return -1 }); got != 0 {
		t.Fatalf("negative draw = %s, want zero", got)
	}
	if got := wakeDelay(time.Second, func(int64) int64 { return 2 }); got != 2 {
		t.Fatalf("valid draw = %s, want 2ns", got)
	}
	if got := wakeDelay(time.Second, func(int64) int64 { return 2_000_000_000 }); got != time.Second {
		t.Fatalf("oversized draw = %s, want interval", got)
	}
}

func TestWakeLoopRejectsMissingPollCallback(t *testing.T) {
	loop := newWakeLoop(time.Second, nil, nil, nil)
	if err := loop.run(context.Background(), nil, nil); err == nil {
		t.Fatal("missing poll callback was accepted")
	}
}

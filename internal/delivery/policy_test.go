package delivery

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestPolicyLaddersAndInclusiveClamp(t *testing.T) {
	base, cap := 5*time.Second, 20*time.Second
	for _, tc := range []struct {
		name, mode string
		want       []time.Duration
	}{{"fixed", "fixed", []time.Duration{5, 5, 5}}, {"linear", "linear", []time.Duration{5, 10, 15}},
		{"exponential", "exponential", []time.Duration{5, 10, 20}}} {
		p := Policy{Backoff: tc.mode, InitialInterval: base, MaxInterval: cap}
		for attempt, want := range tc.want {
			if got := p.Ladder(attempt + 1); got != want*time.Second {
				t.Errorf("%s attempt %d: got %s, want %s", tc.name, attempt+1, got, want*time.Second)
			}
		}
	}
	for _, tc := range []struct {
		name string
		p    Policy
		try  int
		want time.Duration
	}{
		{"fixed below", Policy{Backoff: "fixed", InitialInterval: 19 * time.Second, MaxInterval: 20 * time.Second}, 1, 19 * time.Second},
		{"fixed equal", Policy{Backoff: "fixed", InitialInterval: 20 * time.Second, MaxInterval: 20 * time.Second}, 1, 20 * time.Second},
		{"fixed above", Policy{Backoff: "fixed", InitialInterval: 21 * time.Second, MaxInterval: 20 * time.Second}, 1, 20 * time.Second},
		{"linear below", Policy{Backoff: "linear", InitialInterval: 19 * time.Second, MaxInterval: 20 * time.Second}, 1, 19 * time.Second},
		{"linear equal", Policy{Backoff: "linear", InitialInterval: 10 * time.Second, MaxInterval: 20 * time.Second}, 2, 20 * time.Second},
		{"linear above", Policy{Backoff: "linear", InitialInterval: 11 * time.Second, MaxInterval: 20 * time.Second}, 2, 20 * time.Second},
		{"exponential below", Policy{Backoff: "exponential", InitialInterval: 19 * time.Second, MaxInterval: 20 * time.Second}, 1, 19 * time.Second},
		{"exponential equal", Policy{Backoff: "exponential", InitialInterval: 10 * time.Second, MaxInterval: 20 * time.Second}, 2, 20 * time.Second},
		{"exponential above", Policy{Backoff: "exponential", InitialInterval: 11 * time.Second, MaxInterval: 20 * time.Second}, 2, 20 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.Ladder(tc.try); got != tc.want {
				t.Fatalf("attempt %d = %s, want %s", tc.try, got, tc.want)
			}
		})
	}
	if (Policy{Backoff: "unknown", InitialInterval: base, MaxInterval: cap}).Ladder(2) !=
		(Policy{Backoff: "fixed", InitialInterval: base, MaxInterval: cap}).Ladder(2) {
		t.Fatal("unknown backoff did not follow documented fixed fallback")
	}
	for _, tc := range []struct {
		exponent int
		want     int64
	}{
		{61, 1 << 61}, {62, 1 << 62}, {63, maxDurationInt64}, {int(^uint(0) >> 1), maxDurationInt64}} {
		if got := powerOfTwo(tc.exponent); got != tc.want {
			t.Errorf("powerOfTwo(%d) = %d, want %d", tc.exponent, got, tc.want)
		}
	}
}
func TestPolicyFullJitterUsesInjectedDrawAndDistribution(t *testing.T) {
	p := Policy{Backoff: "fixed", InitialInterval: time.Second, MaxInterval: time.Second, Jitter: true}
	if got := p.DelayWith(1, func(max int64) int64 { return max - 1 }); got != time.Second {
		t.Fatalf("inclusive draw = %s, want 1s", got)
	}
	seed, count, sum, min, max := uint64(17), 2000, int64(0), int64(1<<62), int64(-1)
	for i := 0; i < count; i++ {
		seed = seed*6364136223846793005 + 1
		got := p.DelayWith(1, func(limit int64) int64 { return int64(seed % uint64(limit)) })
		n := int64(got)
		if n < 0 || n > int64(p.Ladder(1)) {
			t.Fatalf("jitter sample %d outside [0,%d]", n, p.Ladder(1))
		}
		sum += n
		if n < min {
			min = n
		}
		if n > max {
			max = n
		}
	}
	if min > int64(50*time.Millisecond) || max < int64(950*time.Millisecond) || float64(sum)/float64(count) < 0.45*float64(time.Second) ||
		float64(sum)/float64(count) > 0.55*float64(time.Second) {
		t.Fatalf("jitter sample min=%d max=%d mean=%g, want [0,%d] near half", min, max,
			float64(sum)/float64(count), time.Second)
	}
}
func TestPolicyTerminalEqualityAndRetryAfter(t *testing.T) {
	p := Policy{MaxAttempts: 3, Backoff: "fixed", InitialInterval: time.Second, MaxInterval: 10 * time.Second}
	for _, tc := range []struct {
		attempt int
		retry   bool
	}{{1, true}, {2, true}, {3, false}, {4, false}} {
		if got := p.ShouldRetry(tc.attempt, OutcomeRetryable); got != tc.retry {
			t.Errorf("attempt %d retryable = %t, want %t", tc.attempt, got, tc.retry)
		}
		if p.ShouldRetry(tc.attempt, OutcomeSuccess) || p.ShouldRetry(tc.attempt, OutcomeTerminal) {
			t.Errorf("attempt %d non-retryable outcome was retried", tc.attempt)
		}
	}
	jittered := Policy{Backoff: "exponential", InitialInterval: 5 * time.Second,
		MaxInterval: 20 * time.Second, Jitter: true}
	draw := func(limit int64) int64 { return 9 * int64(time.Second) % limit }
	if got := jittered.DelayAfter(3, 15*time.Second, draw); got != 15*time.Second {
		t.Fatalf("composed jitter/floor got %s, want 15s", got)
	}
	if got := jittered.DelayAfter(3, 30*time.Second, draw); got != 20*time.Second {
		t.Fatalf("composed jitter/cap got %s, want 20s", got)
	}
	off := Policy{Backoff: "fixed", InitialInterval: 10 * time.Second, MaxInterval: 20 * time.Second}
	if got := off.DelayWith(1, func(int64) int64 { t.Fatal("jitter draw used while disabled"); return 0 }); got != 10*time.Second {
		t.Fatalf("jitter off got %s, want 10s", got)
	}
	for retry, want := range map[time.Duration]time.Duration{0: 10 * time.Second, 5 * time.Second: 10 * time.Second,
		10 * time.Second: 10 * time.Second, 15 * time.Second: 15 * time.Second} {
		if got := off.DelayAfter(1, retry, nil); got != want {
			t.Errorf("Retry-After %s got %s, want %s", retry, got, want)
		}
	}
	if retry, ok := ParseRetryAfter("not-a-date", time.Time{}); ok || off.DelayAfter(1, retry, nil) != 10*time.Second {
		t.Fatalf("malformed Retry-After was not ignored: %s, %t", retry, ok)
	}
	maxPolicy := Policy{Backoff: "fixed", InitialInterval: time.Duration(maxDurationInt64),
		MaxInterval: time.Duration(maxDurationInt64), Jitter: true}
	if got := maxPolicy.DelayWith(1, func(limit int64) int64 {
		if limit != maxDurationInt64 {
			t.Fatalf("overflow bound = %d, want MaxInt64", limit)
		}
		return limit - 1
	}); got != time.Duration(maxDurationInt64-1) {
		t.Fatalf("MaxInt64 jitter = %d, want MaxInt64-1", got)
	}
}
func TestParseRetryAfterFiveForms(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	future := now.Add(30 * time.Second).Format(http.TimeFormat)
	cases := []struct {
		name, value string
		ok          bool
		want        time.Duration
	}{
		{"delta", "30", true, 30 * time.Second},
		{"date", future, true, 30 * time.Second},
		{"past date", now.Add(-time.Second).Format(http.TimeFormat), false, 0},
		{"negative", "-1", false, 0},
		{"malformed", "tomorrow", false, 0},
		{"oversized delta saturates", "999999999999999999999999", true, time.Duration(1<<63 - 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseRetryAfter(tc.value, now)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Fatalf("ParseRetryAfter(%q) = %s, %t; want %s, %t", tc.value, got, ok, tc.want, tc.ok)
			}
		})
	}
}
func TestClassifyStatusIsTotalAndFailClosed(t *testing.T) {
	counts := map[Outcome]int{}
	for status := 100; status <= 599; status++ {
		outcome := ClassifyStatus(status)
		counts[outcome]++
		if outcome != OutcomeSuccess && outcome != OutcomeRetryable && outcome != OutcomeTerminal {
			t.Fatalf("status %d returned unknown outcome %d", status, outcome)
		}
	}
	if counts[OutcomeSuccess] != 100 || counts[OutcomeRetryable] != 102 || counts[OutcomeTerminal] != 298 {
		t.Fatalf("classification counts = %v, want success=100 retryable=102 terminal=298", counts)
	}
	for _, tc := range []struct {
		name   string
		status int
		want   Outcome
	}{
		{"100 lower terminal", 100, OutcomeTerminal}, {"199 upper 1xx terminal", 199, OutcomeTerminal},
		{"200 lower success", 200, OutcomeSuccess}, {"299 upper success", 299, OutcomeSuccess},
		{"300 lower 3xx terminal", 300, OutcomeTerminal}, {"399 upper 3xx terminal", 399, OutcomeTerminal},
		{"400 lower 4xx terminal", 400, OutcomeTerminal}, {"499 upper 4xx terminal", 499, OutcomeTerminal},
		{"407 auth 4xx terminal", 407, OutcomeTerminal}, {"408 timeout retryable", 408, OutcomeRetryable},
		{"429 rate limit retryable", 429, OutcomeRetryable},
		{"500 lower retryable", 500, OutcomeRetryable}, {"599 upper retryable", 599, OutcomeRetryable},
		{"600 unrecognized terminal", 600, OutcomeTerminal}, {"negative unrecognized", -1, OutcomeTerminal},
	} {
		if got := ClassifyStatus(tc.status); got != tc.want {
			t.Errorf("%s: status %d = %s, want %s", tc.name, tc.status, got, tc.want)
		}
	}
	for _, tc := range []struct {
		outcome Outcome
		want    string
	}{{OutcomeSuccess, "success"},
		{OutcomeRetryable, "retryable"}, {OutcomeTerminal, "terminal"}} {
		if got := tc.outcome.String(); got != tc.want {
			t.Errorf("Outcome(%d).String() = %q, want %q", tc.outcome, got, tc.want)
		}
	}
	if ClassifyError(errors.New("timeout")) != OutcomeRetryable || ClassifyError(nil) != OutcomeTerminal {
		t.Fatal("transport error classifier is not retryable and nil is not terminal")
	}
}

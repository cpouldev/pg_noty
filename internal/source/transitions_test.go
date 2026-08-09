package source

import (
	"testing"
	"time"
)

func TestRetryDelayUsesTheInjectableClockAndClampsPastTimes(t *testing.T) {
	now := time.Unix(100, 0)
	source := &TriggerSource{now: func() time.Time { return now }}
	if got := retryDelay(source, now.Add(-time.Second)); got != 0 {
		t.Fatalf("past retry delay = %s, want zero", got)
	}
	if got := retryDelay(source, now.Add(5*time.Second)); got != 5*time.Second {
		t.Fatalf("future retry delay = %s, want 5s", got)
	}
}

package source

import (
	"context"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
)

func TestTriggerSourceHasAnInjectableClockAndIdempotentClose(t *testing.T) {
	source, err := Open(context.Background(), nil, config.Config{}, Options{Listen: false})
	if err != nil {
		t.Fatal(err)
	}
	if source.now == nil {
		t.Fatal("TriggerSource clock is nil")
	}
	want := time.Unix(123, 456)
	source.now = func() time.Time { return want }
	if got := source.now(); !got.Equal(want) {
		t.Fatalf("injectable clock = %v, want %v", got, want)
	}
	if source.Notify() == nil {
		t.Fatal("Notify returned a nil wake-up channel")
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case _, open := <-source.Notify():
		if open {
			t.Fatal("closed source wake-up channel still accepts values")
		}
	default:
		t.Fatal("Close did not close the wake-up channel")
	}
	if err := source.Close(); err != nil {
		t.Fatalf("second Close returned %v", err)
	}
}

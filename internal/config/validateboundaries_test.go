package config

import (
	"testing"
	"time"
)

func TestStageHAcceptsEveryOwnedBoundaryAtItsWrittenMagnitude(t *testing.T) {
	text := stageHExtensionSafeValue("  concurrency: 1024\n")
	replacements := [][2]string{
		{"batch_size: 100", "batch_size: 10000"},
		{"poll_interval: 1s", "poll_interval: 500ms"},
		{"keep: 168h", "keep: 168h"},
		{"timeout: 10s", "timeout: 1h30m"},
		{"max_attempts: 5", "max_attempts: 1"},
		{"max_bytes: 4096", "max_bytes: 262144"},
		{stageHListenerConcurrency, "concurrency: 1024"},
	}
	for _, replacement := range replacements {
		if replacement[0] != replacement[1] {
			text = replaceOnce(t, text, replacement[0], replacement[1])
		}
	}

	raw, diags := stageH(t, text)
	if len(diags) != 0 {
		t.Fatalf("accepting boundaries produced %q", messagesOf(diags))
	}
	got := []struct {
		name string
		got  any
		want any
	}{
		{"worker.concurrency", raw.Worker.Value.Concurrency.value, 1024},
		{"worker.batch_size", raw.Worker.Value.BatchSize.value, 10000},
		{"worker.poll_interval", raw.Worker.Value.PollInterval.value, 500 * time.Millisecond},
		{"retention.keep", raw.Retention.Value.Keep.value, 168 * time.Hour},
		{"defaults.timeout", raw.Defaults.Value.Timeout.value, 90 * time.Minute},
		{"retry.max_attempts", raw.Defaults.Value.Retry.Value.MaxAttempts.value, 1},
		{"payload.max_bytes", raw.Listeners.Values[0].Payload.Value.MaxBytes.value, 262144},
		{"listener concurrency", raw.Listeners.Values[0].Concurrency.value, 1024},
	}
	for _, value := range got {
		if value.got != value.want {
			t.Errorf("%s = %v, want %v", value.name, value.got, value.want)
		}
	}
}

func TestIntegerRangesCloseBothSidesOfEveryBoundary(t *testing.T) {
	base := stageHExtensionSafeValue("  concurrency: 1\n")
	tests := []struct {
		name, from, to string
		want           int
	}{
		{"worker lower", "  concurrency: 1\n", "  concurrency: 1\n", 0},
		{"worker upper", "  concurrency: 1\n", "  concurrency: 1024\n", 0},
		{"batch lower", "batch_size: 100", "batch_size: 1", 0},
		{"batch upper", "batch_size: 100", "batch_size: 10000", 0},
		{"batch above", "batch_size: 100", "batch_size: 10001", 1},
		{"max bytes lower", "max_bytes: 4096", "max_bytes: 1", 0},
		{"listener lower", stageHListenerConcurrency, "concurrency: 1", 0},
		{"listener upper", stageHListenerConcurrency, "concurrency: 1024", 0},
		{"listener below", stageHListenerConcurrency, "concurrency: 0", 1},
		{"listener above", stageHListenerConcurrency, "concurrency: 1025", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text := base
			if tc.from != tc.to {
				text = replaceOnce(t, text, tc.from, tc.to)
			}
			_, diags := stageH(t, text)
			if len(diags) != tc.want {
				t.Errorf("got %d diagnostics %q, want %d", len(diags), messagesOf(diags), tc.want)
			}
		})
	}
}

func TestNameLengthBoundaryIsOneLeadingPlusFortyFollowingCharacters(t *testing.T) {
	base := stageHExtensionSafeValue("  concurrency: 8\n")
	for _, tc := range []struct {
		length int
		want   int
	}{
		{41, 0},
		{42, 1},
	} {
		t.Run(string(rune('0'+tc.want))+" diagnostics", func(t *testing.T) {
			name := "a" + string(make([]byte, tc.length-1))
			name = replaceNULs(name, 'a')
			_, diags := stageH(t, replaceOnce(t, base, "instance: noty", "instance: "+name))
			if len(diags) != tc.want {
				t.Errorf("length %d produced %q, want %d diagnostics", tc.length, messagesOf(diags), tc.want)
			}
		})
	}
}

func replaceNULs(text string, with byte) string {
	buf := []byte(text)
	for i := range buf {
		if buf[i] == 0 {
			buf[i] = with
		}
	}
	return string(buf)
}

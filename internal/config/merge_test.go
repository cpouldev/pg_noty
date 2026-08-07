package config

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestRetryAndTimeoutMergeAcrossAllThreeLayers(t *testing.T) {
	tests := []struct {
		name        string
		defaults    string
		listener    string
		wantTimeout time.Duration
		wantRetry   Retry
	}{
		{
			name:        "built-in",
			wantTimeout: 5 * time.Second,
			wantRetry: Retry{MaxAttempts: 5, Backoff: "exponential",
				InitialInterval: 10 * time.Second, MaxInterval: time.Hour, Jitter: true},
		},
		{
			name: "defaults block",
			defaults: "defaults:\n  timeout: 6s\n  retry:\n    max_attempts: 6\n" +
				"    backoff: linear\n    initial_interval: 11s\n    max_interval: 2h\n    jitter: false\n",
			wantTimeout: 6 * time.Second,
			wantRetry: Retry{MaxAttempts: 6, Backoff: "linear",
				InitialInterval: 11 * time.Second, MaxInterval: 2 * time.Hour, Jitter: false},
		},
		{
			name: "listener",
			defaults: "defaults:\n  timeout: 6s\n  retry:\n    max_attempts: 6\n" +
				"    backoff: linear\n    initial_interval: 11s\n    max_interval: 2h\n    jitter: false\n",
			listener: "    timeout: 7s\n    retry:\n      max_attempts: 7\n" +
				"      backoff: fixed\n      initial_interval: 12s\n      max_interval: 3h\n      jitter: true\n",
			wantTimeout: 7 * time.Second,
			wantRetry: Retry{MaxAttempts: 7, Backoff: "fixed",
				InitialInterval: 12 * time.Second, MaxInterval: 3 * time.Hour, Jitter: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := resolvedMergeDocument(t, tc.defaults, tc.listener)
			got := cfg.Listeners[0].Delivery
			if got.Timeout != tc.wantTimeout {
				t.Errorf("timeout = %s, want %s", got.Timeout, tc.wantTimeout)
			}
			if !reflect.DeepEqual(got.Retry, tc.wantRetry) {
				t.Errorf("retry = %+v, want %+v", got.Retry, tc.wantRetry)
			}
		})
	}
}

func TestRetryMergeClosesTheOneAndFiveFieldOverrideBoundaries(t *testing.T) {
	defaults := "defaults:\n  retry:\n    max_attempts: 6\n    backoff: linear\n" +
		"    initial_interval: 11s\n    max_interval: 2h\n    jitter: false\n"
	tests := []struct {
		name, listener string
		want           Retry
	}{
		{
			name:     "one of five",
			listener: "    retry:\n      max_attempts: 10\n",
			want: Retry{MaxAttempts: 10, Backoff: "linear",
				InitialInterval: 11 * time.Second, MaxInterval: 2 * time.Hour, Jitter: false},
		},
		{
			name: "five of five",
			listener: "    retry:\n      max_attempts: 8\n      backoff: fixed\n" +
				"      initial_interval: 12s\n      max_interval: 3h\n      jitter: true\n",
			want: Retry{MaxAttempts: 8, Backoff: "fixed",
				InitialInterval: 12 * time.Second, MaxInterval: 3 * time.Hour, Jitter: true},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolvedMergeDocument(t, defaults, tc.listener).Listeners[0].Delivery.Retry
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("retry = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestExplicitFalseAndZeroSurvivePresenceDrivenMerge(t *testing.T) {
	cfg := resolvedMergeDocument(t,
		"defaults:\n  retry:\n    max_attempts: 9\n    jitter: true\n",
		"    retry:\n      jitter: false\n")
	if cfg.Listeners[0].Delivery.Retry.Jitter {
		t.Error("listener jitter = true, want explicitly written false")
	}

	raw, diags := stageH(t, mergeDocument(
		"defaults:\n  retry:\n    max_attempts: 9\n",
		"    retry:\n      max_attempts: 0\n"))
	if len(diags) != 1 || diags[0].Rule != R14 {
		t.Fatalf("zero fixture diagnostics = %+v, want its one R14 validation finding", diags)
	}
	if got := resolveConfig(raw).Listeners[0].Delivery.Retry.MaxAttempts; got != 0 {
		t.Errorf("merged max_attempts = %d, want explicitly written zero", got)
	}
}

func TestHeadersMergeByCanonicalNameWithTheListenerWinning(t *testing.T) {
	defaults := "defaults:\n  headers:\n    User-Agent: inherited\n    X-Default: kept\n"
	listener := "    destination:\n      url: https://hooks.example.test/orders\n" +
		"      headers:\n        user-agent: listener\n        x-listener: added\n"
	cfg := resolvedMergeDocument(t, defaults, listener)
	got := cfg.Listeners[0].Delivery.Destination.Headers
	want := Headers{"User-Agent": "listener", "X-Default": "kept", "X-Listener": "added"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("headers = %#v, want %#v", got, want)
	}
	for name := range got {
		canonical, valid := canonicalHTTPFieldName(name)
		if !valid || canonical != name {
			t.Errorf("stored header %q is not one valid canonical field name", name)
		}
	}
}

func TestResolutionCanonicalizesAnUnorderedRawOperationSlice(t *testing.T) {
	writtenName := func(name string) Str {
		return Str{presence: presence{Set: true}, value: name}
	}
	raw := rawOperations{values: []namedOperation{
		{Name: writtenName("delete")},
		{Name: writtenName("insert")},
		{Name: writtenName("update")},
	}}

	if got := operationKinds(resolveOperations(raw)); !reflect.DeepEqual(got,
		[]string{"insert", "update", "delete"}) {
		t.Errorf("operation order = %v, want insert, update, delete", got)
	}
}

func resolvedMergeDocument(t *testing.T, defaults, listener string) *Config {
	t.Helper()
	raw, diags := stageH(t, mergeDocument(defaults, listener))
	if len(diags) != 0 {
		t.Fatalf("merge fixture has diagnostics: %+v", diags)
	}
	return resolveConfig(raw)
}

func mergeDocument(defaults, listener string) string {
	destination := "    destination:\n      url: https://hooks.example.test/orders\n"
	if listener != "" && containsDestination(listener) {
		destination = ""
	}
	return fmt.Sprintf("version: 1\ndatabase:\n  url: postgres://noty@db/noty\n%s"+
		"listeners:\n  - name: order_paid\n    table: public.orders\n"+
		"    operations: [insert]\n%s%s", defaults, destination, listener)
}

func containsDestination(text string) bool {
	return len(text) >= len("    destination:") &&
		text[:len("    destination:")] == "    destination:"
}

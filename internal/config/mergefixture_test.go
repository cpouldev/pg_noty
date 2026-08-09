package config

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestMergeFixturesResolveThroughThePublicEntryPoint(t *testing.T) {
	tests := []struct {
		name        string
		wantTimeout time.Duration
		wantRetry   Retry
		wantHeaders Headers
	}{
		{
			name:        "merge_three_layers.yaml",
			wantTimeout: 7 * time.Second,
			wantRetry: Retry{MaxAttempts: 7, Backoff: "fixed",
				InitialInterval: 12 * time.Second, MaxInterval: 3 * time.Hour, Jitter: true},
			wantHeaders: Headers{"User-Agent": "listener", "X-Default": "kept"},
		},
		{
			name:        "merge_partial_retry.yaml",
			wantTimeout: 5 * time.Second,
			wantRetry: Retry{MaxAttempts: 10, Backoff: "linear",
				InitialInterval: 11 * time.Second, MaxInterval: 2 * time.Hour, Jitter: false},
			wantHeaders: Headers{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(validCorpus, tc.name)
			cfg, warnings, errs := Parse(readFixtureBytes(t, path), tc.name, corpusEnvironment())
			if len(errs) != 0 || len(warnings) != 0 || cfg == nil {
				t.Fatalf("Parse returned config=%v warnings=%+v errors=%+v", cfg, warnings, errs)
			}
			got := cfg.Listeners[0].Delivery
			if got.Timeout != tc.wantTimeout || !reflect.DeepEqual(got.Retry, tc.wantRetry) {
				t.Errorf("delivery timeout/retry = %s/%+v, want %s/%+v",
					got.Timeout, got.Retry, tc.wantTimeout, tc.wantRetry)
			}
			if !reflect.DeepEqual(got.Destination.Headers, tc.wantHeaders) {
				t.Errorf("headers = %#v, want %#v", got.Destination.Headers, tc.wantHeaders)
			}
		})
	}
}

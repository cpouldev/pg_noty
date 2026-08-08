package config

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestValidFixturesCoverEveryGeneratedEnumAndBoundaryDimension(t *testing.T) {
	lower := validStageHFixture(t, "R6_ok_worker_concurrency_lower_bound")
	upper := validStageHFixture(t, "R6_ok_worker_concurrency_upper_bound")
	assertIntSet(t, "R6 boundaries",
		[]int{lower.Worker.Value.Concurrency.value, upper.Worker.Value.Concurrency.value},
		[]int{1, 1024})

	backoffs := validStageHFixture(t, "R15_ok_retry_backoffs")
	gotBackoffs := []string{backoffs.Defaults.Value.Retry.Value.Backoff.value}
	for _, listener := range backoffs.Listeners.Values {
		if listener.Retry.Value.Backoff.Set {
			gotBackoffs = append(gotBackoffs, listener.Retry.Value.Backoff.value)
		}
	}
	assertStringSet(t, "R15 enum", gotBackoffs, retryBackoffs)

	methods := validStageHFixture(t, "R37_ok_destination_methods")
	var gotMethods []string
	for _, listener := range methods.Listeners.Values {
		gotMethods = append(gotMethods, listener.Destination.Value.Method.value)
	}
	assertStringSet(t, "R37 enum", gotMethods, destinationMethods)

	payloads := validStageHFixture(t, "R31_ok_payload_modes")
	var gotModes []string
	for _, listener := range payloads.Listeners.Values {
		payload := listener.Payload.Value
		gotModes = append(gotModes, payload.Mode.value)
		if payload.Mode.value == "columns" && (!payload.Columns.Valid() || len(payload.Columns.values) == 0) {
			t.Error("columns mode has no valid, non-empty columns operand")
		}
	}
	assertStringSet(t, "R31 enum", gotModes, payloadModes)
}

func TestValidBooleanFixturesCoverBothValuesAndEveryWrittenExtent(t *testing.T) {
	jitter := validStageHFixture(t, "R18_ok_retry_jitter_boolean")
	listener := firstFixtureListener(t, jitter, "R18_ok_retry_jitter_boolean")
	assertBoolSet(t, "R18 booleans", []bool{
		jitter.Defaults.Value.Retry.Value.Jitter.value,
		listener.Retry.Value.Jitter.value,
	}, []bool{false, true})
	if !jitter.Defaults.Value.Retry.Value.Jitter.Set ||
		!listener.Retry.Value.Jitter.Set {
		t.Error("R18 valid fixture does not write both defaults.retry and listeners[].retry")
	}

	enabled := validStageHFixture(t, "R25_ok_listener_enabled_boolean")
	var gotEnabled []bool
	for _, listener := range enabled.Listeners.Values {
		if !listener.Enabled.Set {
			t.Error("R25 valid fixture leaves enabled absent")
		}
		gotEnabled = append(gotEnabled, listener.Enabled.value)
	}
	assertBoolSet(t, "R25 booleans", gotEnabled, []bool{false, true})

	isDistinct := validStageHFixture(t, "R43_ok_is_distinct_boolean")
	var gotIsDistinct []bool
	for i, listener := range isDistinct.Listeners.Values {
		if len(listener.Operations.values) != 1 ||
			listener.Operations.values[0].Name.value != updateOperation {
			t.Errorf("R43 listener %d does not carry exactly one update operation", i)
			continue
		}
		value := listener.Operations.values[0].Filter.IsDistinct
		if !value.Set {
			t.Errorf("R43 listener %d leaves is_distinct absent", i)
		}
		gotIsDistinct = append(gotIsDistinct, value.value)
	}
	assertBoolSet(t, "R43 booleans", gotIsDistinct, []bool{false, true})
}

func TestRetryFixturesCoverDefaultsAndListenerExtents(t *testing.T) {
	validChecks := []struct {
		fixture string
		written func(rawRetry) bool
	}{
		{"R14_ok_retry_max_attempts_one", func(r rawRetry) bool { return r.MaxAttempts.Set }},
		{"R15_ok_retry_backoffs", func(r rawRetry) bool { return r.Backoff.Set }},
		{"R17_ok_retry_max_interval_positive", func(r rawRetry) bool { return r.MaxInterval.Set }},
		{"R18_ok_retry_jitter_boolean", func(r rawRetry) bool { return r.Jitter.Set }},
	}
	for _, tc := range validChecks {
		raw := validStageHFixture(t, tc.fixture)
		if !tc.written(raw.Defaults.Value.Retry.Value) || len(raw.Listeners.Values) == 0 ||
			!tc.written(raw.Listeners.Values[0].Retry.Value) {
			t.Errorf("%s does not write its rule under both retry extents", tc.fixture)
		}
	}

	wantPaths := map[RuleID][]string{
		R14: {"defaults.retry.max_attempts", "listeners[0].retry.max_attempts"},
		R15: {"defaults.retry.backoff", "listeners[0].retry.backoff"},
		R17: {"defaults.retry.max_interval", "listeners[0].retry.max_interval"},
		R18: {"defaults.retry.jitter", "listeners[0].retry.jitter"},
	}
	for rule, want := range wantPaths {
		var got []string
		for _, fixture := range stageHRejectingFixtures {
			if fixture.rule == rule {
				got = append(got, fixture.path)
			}
		}
		assertStringSet(t, string(rule)+" rejecting extents", got, want)
	}
}

func TestValidDurationFixturesSatisfyDeferredComparisons(t *testing.T) {
	retention := validStageHFixture(t, "R10_ok_retention_durations_positive").Retention.Value
	if !retention.Precreate.Valid() || !retention.PartitionInterval.Valid() ||
		retention.Precreate.value < retention.PartitionInterval.value {
		t.Errorf("precreate %s is below partition_interval %s",
			retention.Precreate.value, retention.PartitionInterval.value)
	}

	retries := validStageHFixture(t, "R17_ok_retry_max_interval_positive")
	listener := firstFixtureListener(t, retries, "R17_ok_retry_max_interval_positive")
	extents := []rawRetry{retries.Defaults.Value.Retry.Value, listener.Retry.Value}
	for i, retry := range extents {
		if !retry.InitialInterval.Valid() || !retry.MaxInterval.Valid() ||
			retry.InitialInterval.value > retry.MaxInterval.value {
			t.Errorf("retry extent %d has initial_interval %s above max_interval %s",
				i, retry.InitialInterval.value, retry.MaxInterval.value)
		}
	}
}

func TestInvalidModeFixtureCarriesACompatibilityOperand(t *testing.T) {
	path := filepath.Join(invalidCorpus, "R31_payload_mode_unknown.yaml")
	raw, diags := stageH(t, string(readFixtureBytes(t, path)))
	if len(diags) != 1 || diags[0].Rule != R31 {
		t.Fatalf("invalid mode fixture produced %q, want only R31", messagesOf(diags))
	}
	payload := raw.Listeners.Values[0].Payload.Value
	if !payload.Exclude.Valid() || len(payload.Exclude.values) == 0 {
		t.Error("invalid mode fixture has no valid compatibility operand")
	}
}

func validStageHFixture(t *testing.T, name string) *rawConfig {
	t.Helper()
	path := filepath.Join(validCorpus, name+fixtureExtension)
	raw, diags := stageH(t, string(readFixtureBytes(t, path)))
	if len(diags) != 0 {
		t.Fatalf("%s produced %q, want a clean stage-H baseline", name, messagesOf(diags))
	}
	return raw
}

func firstFixtureListener(t *testing.T, raw *rawConfig, name string) rawListener {
	t.Helper()
	if len(raw.Listeners.Values) == 0 {
		t.Fatalf("%s has no listener extent", name)
	}
	return raw.Listeners.Values[0]
}

func assertStringSet(t *testing.T, name string, got, want []string) {
	t.Helper()
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	slices.Sort(got)
	slices.Sort(want)
	got = slices.Compact(got)
	want = slices.Compact(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func assertIntSet(t *testing.T, name string, got, want []int) {
	t.Helper()
	got = append([]int(nil), got...)
	want = append([]int(nil), want...)
	slices.Sort(got)
	slices.Sort(want)
	got = slices.Compact(got)
	want = slices.Compact(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func assertBoolSet(t *testing.T, name string, got, want []bool) {
	t.Helper()
	found := make(map[bool]bool, len(got))
	expected := make(map[bool]bool, len(want))
	for _, value := range got {
		found[value] = true
	}
	for _, value := range want {
		expected[value] = true
	}
	if len(found) != len(expected) || found[false] != expected[false] || found[true] != expected[true] {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

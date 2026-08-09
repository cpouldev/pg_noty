package config

import (
	"slices"
	"strings"
	"testing"
)

const stageHListenerConcurrency = "concurrency: 1 # stage-H range-valid baseline"

// stageHExtensionSafeValue retains every wrapper-bearing path from everyKindOfValue, but splits the
// mutually exclusive payload list forms across two individually valid listeners. Stage-H exact
// counts therefore remain valid when Step 9 starts enforcing R32 and R33.
func stageHExtensionSafeValue(extraWorker string) string {
	document := everyKindOfValue(extraWorker)
	const conflictingPayload = "" +
		"    mode: full\n" +
		"    columns: [id, total]\n" +
		"    exclude: [internal_note]\n"
	const fullPayload = "" +
		"    mode: full\n" +
		"    exclude: [internal_note]\n"
	if strings.Count(document, conflictingPayload) != 1 {
		panic("everyKindOfValue no longer has its one wrapper-completeness payload")
	}
	document = strings.Replace(document, conflictingPayload, fullPayload, 1)

	const oldConcurrency = "  concurrency: 4\n"
	const columnsListener = "" +
		"- name: columns_payload\n" +
		"  table: public.order_archive\n" +
		"  operations: [insert]\n" +
		"  payload:\n" +
		"    mode: columns\n" +
		"    columns: [id, total]\n" +
		"  destination:\n" +
		"    url: https://example.test/columns\n"
	if strings.Count(document, oldConcurrency) != 1 {
		panic("everyKindOfValue no longer has its one listener concurrency")
	}
	document = strings.Replace(document, oldConcurrency,
		"  "+stageHListenerConcurrency+"\n"+columnsListener, 1)

	const literalSecrets = "      secrets: [current, previous]\n"
	if strings.Count(document, literalSecrets) != 1 {
		panic("everyKindOfValue no longer has its one signing secret list")
	}
	return strings.Replace(document, literalSecrets, "      secrets: [\"${SIGNING_SECRET}\"]\n", 1)
}

func TestStageHExtensionSafeValueKeepsEveryWrapperPathWithoutPayloadConflict(t *testing.T) {
	document := stageHExtensionSafeValue("  concurrency: 8\n")
	raw, diags := stageH(t, document)
	if len(diags) != 0 {
		t.Fatalf("extension-safe stage-H value produced %q", messagesOf(diags))
	}
	if len(raw.Listeners.Values) != 2 {
		t.Fatalf("baseline decoded %d listeners, want full and columns payload forms", len(raw.Listeners.Values))
	}

	full := raw.Listeners.Values[0].Payload.Value
	columns := raw.Listeners.Values[1].Payload.Value
	if full.Mode.value != "full" || full.Columns.Set || !full.Exclude.Valid() {
		t.Errorf("full payload writes mode=%q, columns=%t, valid exclude=%t",
			full.Mode.value, full.Columns.Set, full.Exclude.Valid())
	}
	if columns.Mode.value != "columns" || !columns.Columns.Valid() || columns.Exclude.Set {
		t.Errorf("columns payload writes mode=%q, valid columns=%t, exclude=%t",
			columns.Mode.value, columns.Columns.Valid(), columns.Exclude.Set)
	}

	want := declaredResolutionPaths(levelRoot, "")
	got := writtenResolutionPaths(t, document)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(slices.Compact(got), slices.Compact(want)) {
		t.Errorf("stage-H baseline writes\n%v\nwant every wrapper path\n%v", got, want)
	}
}

func TestInvalidPayloadModeStopsAtR31AcrossTheStep9ExtensionBoundary(t *testing.T) {
	text := replaceOnce(t, stageHExtensionSafeValue("  concurrency: 8\n"), "mode: full", "mode: sideways")
	_, diags := stageH(t, text)
	if len(diags) != 1 || diags[0].Rule != R31 {
		t.Fatalf("invalid mode produced %q, want only R31", messagesOf(diags))
	}

	text = replaceOnce(t, text, "max_bytes: 4096", "max_bytes: 0")
	_, diags = stageH(t, text)
	if len(diags) != 2 {
		t.Fatalf("invalid mode plus max_bytes produced %q, want independent R31 and R35", messagesOf(diags))
	}
	gotRules := []RuleID{diags[0].Rule, diags[1].Rule}
	slices.Sort(gotRules)
	if !slices.Equal(gotRules, []RuleID{R31, R35}) {
		t.Fatalf("invalid mode plus max_bytes produced %q, want independent R31 and R35", messagesOf(diags))
	}
}

func TestR39ComparisonRequiresBothEndpointsToPassTheirOwnRange(t *testing.T) {
	for _, tc := range []struct {
		value int
		want  bool
	}{
		{0, false},
		{1, true},
		{1024, true},
		{1025, false},
	} {
		if got := concurrencyCanParticipateInComparison(tc.value); got != tc.want {
			t.Errorf("concurrency %d can participate = %t, want %t", tc.value, got, tc.want)
		}
	}

	text := replaceOnce(t, stageHExtensionSafeValue("  concurrency: 1024\n"),
		stageHListenerConcurrency, "concurrency: 1025")
	raw, diags := stageH(t, text)
	if len(diags) != 1 || diags[0].Rule != R39 {
		t.Fatalf("1025 listener produced %q, want one R39 range diagnostic", messagesOf(diags))
	}
	listener := raw.Listeners.Values[0].Concurrency.value
	worker := raw.Worker.Value.Concurrency.value
	if concurrencyCanParticipateInComparison(listener) &&
		concurrencyCanParticipateInComparison(worker) {
		t.Fatalf("listener %d and worker %d reached the deferred comparison prerequisite", listener, worker)
	}
}

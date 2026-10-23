package reconcile

import (
	"slices"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func TestTargetOutcomesAreClosedAndClassifyEachSignalCell(t *testing.T) {
	// assert-a-set-wide-invariant-over-the-set.md makes the zero and size pins test obligations.
	if got, want := len(targetOutcomes), 5; got != want {
		t.Fatalf("target outcome count = %d, want %d", got, want)
	}
	var zero targetOutcome
	if slices.Contains(targetOutcomes[:], zero) {
		t.Fatal("target outcomes contain their unresolved zero value")
	}
	if len(uniqueTargetOutcomes()) != len(targetOutcomes) {
		t.Fatalf("target outcomes are not distinct: %#v", targetOutcomes)
	}
	cases := []struct {
		name  string
		state targetState
		want  targetOutcome
	}{
		{"new name exists", targetState{named: true}, targetNew},
		{
			"healthy OID and same name", targetState{recorded: true, oid: true, named: true, sameName: true},
			targetHealthy,
		},
		{"renamed OID and changed name", targetState{recorded: true, oid: true, named: true}, targetRenamed},
		{"dropped OID and name absent", targetState{recorded: true}, targetDropped},
		{"recreated OID absent and name exists", targetState{recorded: true, named: true}, targetDroppedAndRecreated},
	}
	for _, testCase := range cases {
		t.Run(
			testCase.name, func(t *testing.T) {
				if got := classifyTarget(testCase.state); got != testCase.want {
					t.Fatalf("classifyTarget(%+v) = %q, want %q", testCase.state, got, testCase.want)
				}
			},
		)
	}
}

func TestCompileNamesAGenerationRefusalForItsListener(t *testing.T) {
	listener := config.Listener{
		Name: "orders", Trigger: config.TriggerSpec{
			Operations: config.Operations{{Kind: "not-an-operation"}}, Payload: config.Payload{Mode: "full"},
		},
	}
	_, err := compileListener(
		"alpha",
		"noty",
		listener,
		TargetReading{Schema: "public", Table: "orders", PrimaryKeyColumns: []string{"id"}},
	)
	if err == nil || !strings.Contains(err.Error(), `generate listener "orders"`) {
		t.Fatalf("compileListener() error = %v, want a named generation refusal", err)
	}
}

func uniqueTargetOutcomes() map[targetOutcome]struct{} {
	unique := make(map[targetOutcome]struct{}, len(targetOutcomes))
	for _, outcome := range targetOutcomes {
		unique[outcome] = struct{}{}
	}
	return unique
}

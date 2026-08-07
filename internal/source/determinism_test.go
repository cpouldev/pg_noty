package source

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func generatedText(t *testing.T, request Request) string {
	t.Helper()
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, set := range sets {
		parts = append(parts, goldenText(set))
	}
	return strings.Join(parts, "\n")
}

// generationRepeats is how many times one request is generated before its bytes are believed. Two
// runs are not enough: Go randomises map iteration per range, so a ranged two-entry map agrees with
// itself about half the time and a two-run comparison passes on every other execution. This is the
// behavioural half of the determinism defence, and it is the half that still holds when the map is
// declared outside the generation chain, where the source scan in generationscan_test.go cannot see
// it. At 64 repeats a two-entry map survives with probability 2^-63.
const generationRepeats = 64

// The subject is every golden case rather than one request. The grid crosses all four payload modes,
// and each mode has its own expression builder: a map ranged in the builder for a mode this test
// never reaches changes nothing it can observe, so one `mode: full` request would leave three
// builders outside the claim.
func TestGenerationIsByteDeterministicWithinOneProcess(t *testing.T) {
	for _, testCase := range allGoldenCases() {
		first := generatedText(t, testCase.request)
		for repeat := 1; repeat < generationRepeats; repeat++ {
			if again := generatedText(t, testCase.request); again != first {
				t.Fatalf(
					"%s: generation %d of %d changed one or more of the five statements",
					testCase.name, repeat+1, generationRepeats,
				)
			}
		}
	}
}

func TestGenerationIsByteDeterministicAcrossProcesses(t *testing.T) {
	request := generationRequest(config.Operation{Kind: "update"})
	want := generatedText(t, request)
	command := exec.Command(os.Args[0], "-test.run=^TestGenerateSubprocessHelper$", "-test.v=false")
	command.Env = append(os.Environ(), "PGNOTY_GENERATE_SUBPROCESS=1")
	got, err := command.Output()
	if err != nil {
		t.Fatalf("subprocess generation: %v", err)
	}
	if string(got) != want {
		t.Fatal("subprocess generation differs from in-process generation")
	}
}

func TestGenerateSubprocessHelper(t *testing.T) {
	if os.Getenv("PGNOTY_GENERATE_SUBPROCESS") != "1" {
		return
	}
	request := generationRequest(config.Operation{Kind: "update"})
	if _, err := fmt.Fprint(os.Stdout, generatedText(t, request)); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestGenerationPreservesSuppliedCollectionOrders(t *testing.T) {
	columnsA := generationRequest(config.Operation{Kind: "update"})
	columnsA.Listener.Trigger.Payload = config.Payload{Mode: "columns", Columns: []string{"status", "id"}}
	columnsB := columnsA
	columnsB.Listener.Trigger.Payload.Columns = []string{"id", "status"}
	if generatedText(t, columnsA) == generatedText(t, columnsB) {
		t.Fatal("columns supplied in different specification orders produced the same text")
	}

	excludeA := generationRequest(config.Operation{Kind: "insert"})
	excludeA.Listener.Trigger.Payload.Exclude = []string{"secret", "token"}
	excludeB := excludeA
	excludeB.Listener.Trigger.Payload.Exclude = []string{"token", "secret"}
	if generatedText(t, excludeA) == generatedText(t, excludeB) {
		t.Fatal("exclude supplied in different orders produced the same text")
	}

	keysA := generationRequest(config.Operation{Kind: "insert"})
	keysA.Listener.Trigger.Payload.Mode = "keys_only"
	keysA.Target.PrimaryKeyColumns = []string{"id", "tenant_id"}
	keysB := keysA
	keysB.Target.PrimaryKeyColumns = []string{"tenant_id", "id"}
	if generatedText(t, keysA) == generatedText(t, keysB) {
		t.Fatal("primary keys supplied in different orders produced the same text")
	}
}

// The source-level half of this claim -- that the generation chain holds no map to range and calls
// nothing that reorders a list -- is asserted by
// TestTheGenerationChainDeclaresNoMapAndCallsNoOrderingRoutine in generationscan_test.go, over the
// chain derived from Generate's own call graph rather than over a transcribed file list.

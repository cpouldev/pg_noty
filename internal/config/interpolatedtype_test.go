package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// AC #5 through the corpus: one fixture whose reference resolves to an integer and loads clean, and one
// whose reference resolves to text and yields a single positioned diagnostic naming the expected type.
//
// The wrapper-level cases are conversion_test.go's. What these add is the whole pipeline plus the rendered
// block, which is where "the snippet quotes the reference the author wrote rather than what it resolved
// to" is actually claimed (ADR-5, ADR-7).

// TestTheInterpolatedIntegerFixturesCoverBothOfACFivesCases ties AC #5 to the corpus rather than to a
// unit case alone: one fixture whose reference resolves to an integer and loads clean, and one whose
// reference resolves to text and yields a single positioned diagnostic whose rendered block quotes the
// reference the author wrote rather than what it resolved to.
func TestTheInterpolatedIntegerFixturesCoverBothOfACFivesCases(t *testing.T) {
	t.Run("N=16 yields the integer", func(t *testing.T) {
		path := filepath.Join(validCorpus, "decode_ok_interpolated_value_reaches_its_type.yaml")
		data := readFixtureBytes(t, path)

		_, _, errs := Parse(data, filepath.Base(path), corpusEnvironment())
		if len(errs) != 0 {
			t.Fatalf("the fixture reported %q, want none", messagesOf(errs))
		}

		decoded := decodedConfig(t, string(data))
		if got := decoded.Worker.Value.Concurrency; !got.Valid() || got.value != 16 {
			t.Errorf("worker.concurrency read %d (valid %t), want the integer 16", got.value, got.Valid())
		}
	})

	t.Run("N=abc yields one diagnostic naming the type", func(t *testing.T) {
		path := filepath.Join(invalidCorpus, "decode_interpolated_value_of_the_wrong_type.yaml")
		data := readFixtureBytes(t, path)

		// The empty environment the golden harness renders every rejecting fixture against, so what
		// is asserted here is what the golden holds.
		_, _, errs := Parse(data, filepath.Base(path), MapEnv(nil))

		if len(errs) != 1 {
			t.Fatalf("the fixture reported %q, want exactly one diagnostic", messagesOf(errs))
		}
		if errs[0].Msg != mustBeAnInteger.message {
			t.Errorf("Msg = %q, want %q", errs[0].Msg, mustBeAnInteger.message)
		}
		if errs[0].Path != "worker.concurrency" {
			t.Errorf("Path = %q, want the key AC #5 names", errs[0].Path)
		}
		if rendered := errs.Render(data); !strings.Contains(rendered, "${WORKER_CONCURRENCY:-abc}") {
			t.Errorf("the rendered block does not quote the reference the author wrote:\n%s", rendered)
		}
	})
}

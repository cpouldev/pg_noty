package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTheFallbackBlanksASecretBesideASyntaxError(t *testing.T) {
	path := filepath.Join(invalidCorpus, "secret_beside_syntax_error.yaml")
	data := readFixtureBytes(t, path)
	_, _, errs := Parse(data, filepath.Base(path), MapEnv(nil))
	if len(errs) != 1 {
		t.Fatalf("Parse() returned %d diagnostics, want the one syntax error: %+v",
			len(errs), errs)
	}
	rendered := errs.Render(data)
	if strings.Contains(rendered, "s3cret-literal") {
		t.Errorf("the fallback left the secret in the snippet:\n%s", rendered)
	}
	if !strings.Contains(rendered, "url: [redacted]") {
		t.Errorf("the fallback did not blank the value beneath the sensitive key:\n%s",
			rendered)
	}
}

func TestTheCorpusCoversEveryShapeTheFallbackHadToLearn(t *testing.T) {
	tests := []struct {
		shape     string
		fixture   string
		wantLines []string
	}{
		{
			shape:     "a sensitive key written on a sequence-item line",
			fixture:   "secret_in_a_sequence_item",
			wantLines: []string{"   2 | - url: [redacted]"},
		},
		{
			shape:     "a value written on the lines below a sensitive key",
			fixture:   "secret_on_the_lines_below_a_key",
			wantLines: []string{"   2 |   url:", "   3 |     [redacted]"},
		},
		{
			shape:     "a block scalar and the sibling item after it",
			fixture:   "secret_spanning_lines",
			wantLines: []string{"   3 |   [redacted]", "   4 | [redacted]"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.shape, func(t *testing.T) {
			golden := readGolden(t, goldenFor(fixture(tc.fixture)))
			if strings.Contains(golden, "s3cret") {
				t.Errorf("%s.golden quotes the secret its fixture hides:\n%s",
					tc.fixture, golden)
			}
			for _, want := range tc.wantLines {
				if !strings.Contains(golden, want) {
					t.Errorf("%s.golden does not hold %q:\n%s",
						tc.fixture, want, golden)
				}
			}
		})
	}
}

func TestTheFallbackBlanksSequenceItemsBeneathASensitiveKey(t *testing.T) {
	source := "signing:\n" +
		"  secrets:\n" +
		"    - " + leakSentinel + "first\n" +
		"    # a comment between items must not end the block\n" +
		"\n" +
		"    - " + leakSentinel + "second\n" +
		"  other: kept\n" +
		"listeners: [unterminated\n"
	rendered := renderEveryLineOf(source)
	if strings.Contains(rendered, leakSentinel) {
		t.Errorf("a sequence item beneath a sensitive key leaked:\n%s", rendered)
	}
	if !strings.Contains(rendered, "other: kept") {
		t.Errorf("the fallback blanked a value that is not beneath a sensitive key:\n%s",
			rendered)
	}
}

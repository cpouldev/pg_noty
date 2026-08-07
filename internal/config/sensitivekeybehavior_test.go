package config

import (
	"strings"
	"testing"
)

func TestTheFallbackBlanksEveryShapeAValueCanBeWrittenIn(t *testing.T) {
	for _, tc := range sensitiveValueShapeCases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(redactBySensitiveKeyName(
				splitLines(tc.source), carriageReturnSplits(tc.source)), "\n")
			if got != tc.want {
				t.Errorf("the fallback rewrote\n%q\nas\n%q\nwant\n%q",
					tc.source, got, tc.want)
			}
		})
	}
}

func TestTwoSensitiveKeysOnOneLineBothGoBecauseTheEarliestEndIsTaken(t *testing.T) {
	const line = "{url: first-s3cret, secrets: [second-s3cret]}"
	at, sensitive := earliestSensitiveKeyEnd(line)
	if !sensitive {
		t.Fatalf("earliestSensitiveKeyEnd(%q) found no sensitive key", line)
	}
	if want := byteOffset(len("{url:")); at != want {
		t.Errorf("earliestSensitiveKeyEnd(%q) = %d, want %d, just past the earliest key's colon",
			line, at, want)
	}
	if blanked := blankValueAfter(line, at); strings.Contains(blanked, "s3cret") {
		t.Errorf("blanking from the earliest sensitive key left a secret: %q", blanked)
	}
}

func TestEverySpellingOfEverySensitiveKeyIsRecognised(t *testing.T) {
	if len(sensitiveKeyNames) == 0 {
		t.Fatal("the schema table declares no sensitive key, so this test would pass vacuously")
	}
	for _, name := range sensitiveKeyNames {
		for _, spelling := range keySpellings {
			t.Run(name+", "+spelling.name, func(t *testing.T) {
				line := "  " + spelling.write(name) + " s3cret"
				at, sensitive := earliestSensitiveKeyEnd(line)
				if !sensitive {
					t.Fatalf("earliestSensitiveKeyEnd(%q) reports no sensitive key", line)
				}
				got := leadingTokenOf(strings.TrimLeft(line, indentCharacters))
				if got != aKeyThisFileCanRead {
					t.Errorf("leadingTokenOf(%q) = %d, want aKeyThisFileCanRead; the two readers of a key disagree",
						line, got)
				}
				if blanked := blankValueAfter(line, at); strings.Contains(blanked, "s3cret") {
					t.Errorf("blanking from the recognised key left the value: %q", blanked)
				}
			})
		}
	}
}

func TestTheFallbackReadsEverySpellingAKeyCanBeWrittenIn(t *testing.T) {
	for _, tc := range sensitiveKeySpellingCases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(redactBySensitiveKeyName(
				splitLines(tc.source), carriageReturnSplits(tc.source)), "\n")
			if got != tc.want {
				t.Errorf("the fallback rewrote\n%q\nas\n%q\nwant\n%q",
					tc.source, got, tc.want)
			}
			if strings.Contains(got, "s3cret") {
				t.Errorf("the fallback left a secret in\n%q", got)
			}
		})
	}
}

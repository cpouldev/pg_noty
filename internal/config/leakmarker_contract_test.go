package config

import (
	"strconv"
	"strings"
	"testing"
)

func TestEveryLineOfAPlantedSecretIsWatched(t *testing.T) {
	if len(secretTails) == 0 {
		t.Fatal("no secret class is enumerated, so this test would pass vacuously")
	}

	for _, tail := range secretTails {
		t.Run(strconv.Quote(tail), func(t *testing.T) {
			marked := secretMarkedOnEveryLine(tail)
			lines := linesWithTheirBreaks(marked.text)

			for number, line := range lines {
				if !strings.Contains(line.content, leakSentinel) {
					t.Errorf("line %d of the planted secret carries no marker: %q", number+1, line.content)
				}
			}
			if want := 2 * len(lines); len(marked.markers) != want {
				t.Errorf("%d markers reported for %d lines, want %d", len(marked.markers), len(lines), want)
			}
			for _, marker := range marked.markers {
				if !strings.Contains(marked.text, marker) {
					t.Errorf("marker %q is reported but never planted in %q", marker, marked.text)
				}
			}
		})
	}
}

func TestMarkingASecretOnlyAddsToIt(t *testing.T) {
	for _, tail := range secretTails {
		t.Run(strconv.Quote(tail), func(t *testing.T) {
			marked := secretMarkedOnEveryLine(tail)

			stripped := marked.text
			for _, marker := range marked.markers {
				stripped = strings.ReplaceAll(stripped, marker, "")
			}
			if stripped != tail {
				t.Errorf("stripping the markers from %q gives %q, want the secret's own bytes %q",
					marked.text, stripped, tail)
			}
		})
	}
}

func TestAWatchedPhysicalTailIsMarkedAtBothEnds(t *testing.T) {
	const tail = "ordinary bytes after a secret"
	watched := textMarkedAtBothEnds("TEST", tail)

	if len(watched.markers) != 2 {
		t.Fatalf("%d markers, want one at each end", len(watched.markers))
	}
	stripped := watched.text
	for _, marker := range watched.markers {
		if !strings.Contains(watched.text, marker) {
			t.Errorf("marker %q is reported but not planted", marker)
		}
		stripped = strings.ReplaceAll(stripped, marker, "")
	}
	if stripped != tail {
		t.Errorf("stripping the markers gives %q, want %q", stripped, tail)
	}
}

func TestNoMarkerIsASubstringOfAnother(t *testing.T) {
	const lines = 12
	marked := secretMarkedOnEveryLine(strings.Repeat("a line\n", lines-1) + "the last")

	if want := 2 * lines; len(marked.markers) != want {
		t.Fatalf("%d markers for a %d-line secret, want %d", len(marked.markers), lines, want)
	}
	for _, marker := range marked.markers {
		if planted := strings.Count(marked.text, marker); planted != 1 {
			t.Errorf("marker %q occurs %d times in the planted secret, want 1", marker, planted)
		}
	}
}

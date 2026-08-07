package delivery

import (
	"strings"
	"testing"
)

func TestTruncateSnippetAdjacentByteBoundaries(t *testing.T) {
	for _, size := range []int{2047, 2048, 2049} {
		t.Run(strconvItoa(size), func(t *testing.T) {
			body := []byte(strings.Repeat("a", size))
			got := TruncateSnippet(body)
			if len(got) != min(size, 2048) {
				t.Fatalf("snippet bytes = %d, want %d", len(got), min(size, 2048))
			}
		})
	}
}

func TestTruncateSnippetRejectsMarkersAtEveryCapExtent(t *testing.T) {
	const marker = "SECRET-MARKER"
	for _, tc := range []struct {
		name  string
		start int
	}{
		{"straddles-cap", 2048 - len(marker)/2},
		{"starts-at-cap", 2048},
		{"starts-after-cap", 2049},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(strings.Repeat("a", 2049+len(marker)))
			copy(body[tc.start:], marker)
			got := TruncateSnippet(body)
			if strings.Contains(got, marker) {
				t.Fatalf("marker at byte %d crossed the rendered cap", tc.start)
			}
		})
	}
}

func TestTruncateSnippetDoesNotMutateInput(t *testing.T) {
	body := []byte(strings.Repeat("z", 2050))
	before := string(body)
	if got := TruncateSnippet(body); len(got) != 2048 {
		t.Fatalf("snippet length = %d", len(got))
	}
	if string(body) != before {
		t.Fatal("snippet truncation mutated input")
	}
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func strconvItoa(value int) string {
	if value == 2047 {
		return "2047"
	}
	if value == 2048 {
		return "2048"
	}
	return "2049"
}

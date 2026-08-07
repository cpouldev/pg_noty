package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestUnreadableKeyRedactionCarriesAcrossItsLoneCarriageReturnTail(t *testing.T) {
	const tail = "PGNOTY-UNREADABLE-KEY-PHYSICAL-TAIL"
	document := `"u\x72l": secret` + "\r" + tail + "\nlisteners: [\n"

	assertFallbackPhysicalTailIsHidden(t, document, []string{tail})
}

func TestEveryUnreadableKeyShapeOwnsItsWholePhysicalLine(t *testing.T) {
	keyShapes := []struct {
		name   string
		prefix string
		line   string
	}{
		{name: "escaped double quoted key", line: `"u\x72l": secret`},
		{name: "doubled single quoted key", line: `'u''rl': secret`},
		{
			name:   "alias key",
			prefix: "key_name: &sensitive_name secrets\n",
			line:   "*sensitive_name : secret",
		},
	}
	tails := []struct {
		name  string
		write func(int) string
	}{
		{name: "ordinary tail", write: func(index int) string {
			return fmt.Sprintf("PGNOTY-PHYSICAL-TAIL-%d", index)
		}},
		{name: "key shaped tail", write: func(index int) string {
			return fmt.Sprintf("ordinary_%d: PGNOTY-PHYSICAL-TAIL-%d", index, index)
		}},
	}

	for _, key := range keyShapes {
		for chain := 1; chain <= 3; chain++ {
			for _, tail := range tails {
				name := fmt.Sprintf("%s/%d returns/%s", key.name, chain, tail.name)
				t.Run(name, func(t *testing.T) {
					var line strings.Builder
					line.WriteString(key.prefix + key.line)
					markers := make([]string, 0, chain)
					for index := 1; index <= chain; index++ {
						written := tail.write(index)
						line.WriteString("\r" + written)
						markers = append(markers,
							fmt.Sprintf("PGNOTY-PHYSICAL-TAIL-%d", index))
					}
					line.WriteString("\nlisteners: [\n")
					assertFallbackPhysicalTailIsHidden(t, line.String(), markers)
				})
			}
		}
	}
}

func assertFallbackPhysicalTailIsHidden(t *testing.T, document string, markers []string) {
	t.Helper()
	text := newSource("listeners.yaml", []byte(document))
	root, diags := parseDocument(text)
	if pathsAreResolvable(root, diags) {
		t.Fatal("fixture does not force fallback redaction")
	}
	rendered := renderEveryLineOf(document)
	for _, marker := range markers {
		if strings.Contains(rendered, marker) {
			t.Errorf("physical-line provenance lost before %s:\n%s", marker, rendered)
		}
	}
}

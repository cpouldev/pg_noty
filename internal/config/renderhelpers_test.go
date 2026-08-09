package config

import (
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func numberedLines(last int) string {
	var text strings.Builder
	for number := 1; number <= last; number++ {
		text.WriteString("line " + strconv.Itoa(number) + "\n")
	}
	return text.String()
}

func linesEndingWith(tail []string, last int) string {
	var text strings.Builder
	for number := 1; number <= last-len(tail); number++ {
		text.WriteString("padding\n")
	}
	for _, line := range tail {
		text.WriteString(line + "\n")
	}
	return text.String()
}

func blockLastLine(t *testing.T, block string) int {
	t.Helper()
	header := block[:strings.IndexByte(block, '\n')]
	fields := strings.Split(header, ":")
	if len(fields) != 3 {
		t.Fatalf("header %q is not file:line:col", header)
	}
	number, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("header %q does not name a line: %v", header, err)
	}
	return number
}

func caretColumn(t *testing.T, block string) int {
	t.Helper()
	line := caretLine(t, block)
	return utf8.RuneCountInString(line[:strings.IndexByte(line, '^')]) + 1
}

func hintColumn(t *testing.T, block string) int {
	t.Helper()
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "^") && i+1 < len(lines) {
			hint := lines[i+1]
			return utf8.RuneCountInString(hint) -
				utf8.RuneCountInString(strings.TrimLeft(hint, " ")) + 1
		}
	}
	t.Fatalf("no hint line in:\n%s", block)
	return 0
}

func caretTarget(t *testing.T, block string) rune {
	t.Helper()
	marked := ""
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, offendingMarker) {
			marked = line
			break
		}
	}
	if marked == "" {
		t.Fatalf("no marked snippet line in:\n%s", block)
	}
	column := caretColumn(t, block)
	runes := []rune(marked)
	if column > len(runes) {
		t.Fatalf("caret at column %d, past the %d runes of %q",
			column, len(runes), marked)
	}
	return runes[column-1]
}

func caretLine(t *testing.T, block string) string {
	t.Helper()
	for _, line := range strings.Split(block, "\n") {
		if trimmed := strings.TrimLeft(line, " "); strings.HasPrefix(trimmed, "^") {
			return line
		}
	}
	t.Fatalf("no caret line in:\n%s", block)
	return ""
}

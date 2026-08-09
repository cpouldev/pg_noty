package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestFlowURLCaretAfterPasswordLandsOnTheAtSign(t *testing.T) {
	const source = "database: {url: postgres://u:p@h/x}\n"
	const sourceColumn = 31
	diagnostic := Error{File: "url.yaml", Line: 1, Col: sourceColumn, Msg: "message"}

	block := Errors{diagnostic}.Render([]byte(source))
	if !strings.HasPrefix(block, "url.yaml:1:31\n") {
		t.Errorf("header lost source column 31:\n%s", block)
	}
	if target := caretTarget(t, block); target != '@' {
		t.Errorf("caret target = %q, want '@' after the password replacement:\n%s", target, block)
	}
}

type urlCaretLayout struct {
	name       string
	lineNumber int
	document   func(string) string
	sourceLine func(string) string
}

func TestPasswordRedactionMapsSourceColumnsPiecewise(t *testing.T) {
	for _, layout := range urlCaretLayouts() {
		for _, credential := range []struct {
			name     string
			user     string
			password string
		}{
			{name: "shorter password", user: "u", password: "p"},
			{name: "equal-length password", user: "u", password: "abcdefghij"},
			{name: "longer password", user: "u", password: "abcdefghijklmnop"},
			{name: "multibyte credentials", user: "χρήστης", password: "κωδικός"},
		} {
			testURLCaretPositions(t, layout, credential.name, credential.user, credential.password)
		}
	}
}

func urlCaretLayouts() []urlCaretLayout {
	return []urlCaretLayout{
		{
			name: "block URL", lineNumber: 2,
			document:   func(url string) string { return "database:\n  url: " + url + "\n" },
			sourceLine: func(url string) string { return "  url: " + url },
		},
		{
			name: "flow URL", lineNumber: 1,
			document:   func(url string) string { return "database: {url: " + url + "}\n" },
			sourceLine: func(url string) string { return "database: {url: " + url + "}" },
		},
	}
}

func testURLCaretPositions(
	t *testing.T,
	layout urlCaretLayout,
	credentialName, user, password string,
) {
	t.Helper()
	url := "postgres://" + user + ":" + password + "@h/x"
	line := layout.sourceLine(url)
	urlColumn := sourceColumnOfText(t, line, url)
	userColumn := urlColumn + runeCount("postgres://")
	passwordColumn := userColumn + runeCount(user) + runeCount(":")
	afterColumn := passwordColumn + runeCount(password)
	delta := runeCount(redactionPlaceholder) - runeCount(password)
	positions := []struct {
		name         string
		sourceColumn int
		renderColumn int
		target       rune
	}{
		{
			name: "before password", sourceColumn: userColumn,
			renderColumn: userColumn, target: []rune(user)[0],
		},
		{
			name: "inside password", sourceColumn: passwordColumn + runeCount(password) - 1,
			renderColumn: passwordColumn, target: '[',
		},
		{
			name: "after password", sourceColumn: afterColumn,
			renderColumn: afterColumn + delta, target: '@',
		},
	}

	for _, position := range positions {
		name := layout.name + "/" + credentialName + "/" + position.name
		t.Run(name, func(t *testing.T) {
			diagnostic := Error{
				File: "url.yaml", Line: layout.lineNumber,
				Col: position.sourceColumn, Msg: "message",
			}
			block := Errors{diagnostic}.Render([]byte(layout.document(url)))
			header := fmt.Sprintf("url.yaml:%d:%d\n", layout.lineNumber, position.sourceColumn)
			if !strings.HasPrefix(block, header) {
				t.Errorf("header does not retain source coordinate %q:\n%s", header, block)
			}
			wantCaret := newSnippetBlock(layout.lineNumber).caretColumn(position.renderColumn)
			if got := caretColumn(t, block); got != wantCaret {
				t.Errorf("caret column = %d, want %d after piecewise mapping:\n%s", got, wantCaret, block)
			}
			if got := caretTarget(t, block); got != position.target {
				t.Errorf("caret target = %q, want %q:\n%s", got, position.target, block)
			}
		})
	}
}

func sourceColumnOfText(t *testing.T, line, text string) int {
	t.Helper()
	at := strings.Index(line, text)
	if at < 0 {
		t.Fatalf("%q does not contain %q", line, text)
	}
	return runeCount(line[:at]) + firstColumn
}

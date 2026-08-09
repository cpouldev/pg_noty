package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestFlowEmptyPasswordCaretAtTheBoundaryLandsOnTheAtSign(t *testing.T) {
	const source = "database: {url: postgres://u:@h/x}\n"
	const sourceColumn = 30
	diagnostic := Error{File: "url.yaml", Line: 1, Col: sourceColumn, Msg: "message"}

	block := Errors{diagnostic}.Render([]byte(source))
	if !strings.HasPrefix(block, "url.yaml:1:30\n") {
		t.Errorf("header lost source column 30:\n%s", block)
	}
	if target := caretTarget(t, block); target != '@' {
		t.Errorf("caret target = %q, want '@' after the inserted placeholder:\n%s", target, block)
	}
}

func TestEmptyPasswordInsertionMapsBoundaryColumns(t *testing.T) {
	credentials := []struct {
		name string
		user string
		host string
	}{
		{name: "ASCII credentials", user: "u", host: "h"},
		{name: "multibyte credentials", user: "χρήστης", host: "δοκιμή"},
	}
	for _, layout := range urlCaretLayouts() {
		for _, credential := range credentials {
			testEmptyPasswordColumns(t, layout, credential.name, credential.user, credential.host)
		}
	}
}

func testEmptyPasswordColumns(
	t *testing.T,
	layout urlCaretLayout,
	credentialName, user, host string,
) {
	t.Helper()
	url := "postgres://" + user + ":@" + host + "/x"
	line := layout.sourceLine(url)
	urlColumn := sourceColumnOfText(t, line, url)
	userColumn := urlColumn + runeCount("postgres://")
	boundaryColumn := userColumn + runeCount(user) + runeCount(":")
	hostColumn := boundaryColumn + runeCount("@")
	shift := runeCount(redactionPlaceholder)
	positions := []struct {
		name         string
		sourceColumn int
		renderColumn int
		target       rune
	}{
		{
			name: "before insertion", sourceColumn: userColumn,
			renderColumn: userColumn, target: []rune(user)[0],
		},
		{
			name: "at insertion", sourceColumn: boundaryColumn,
			renderColumn: boundaryColumn + shift, target: '@',
		},
		{
			name: "after insertion", sourceColumn: hostColumn,
			renderColumn: hostColumn + shift, target: []rune(host)[0],
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
				t.Errorf("caret column = %d, want %d after insertion mapping:\n%s",
					got, wantCaret, block)
			}
			if got := caretTarget(t, block); got != position.target {
				t.Errorf("caret target = %q, want %q:\n%s", got, position.target, block)
			}
		})
	}
}

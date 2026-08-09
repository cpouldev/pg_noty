package config

import (
	"fmt"
	"strings"
	"testing"
)

type twoPasswordCredentials struct {
	name              string
	user, first, last string
}

func TestOneURLMapsCaretsAcrossBothPasswordSpans(t *testing.T) {
	testTwoPasswordCaretPositions(t, twoPasswordCredentials{
		name: "reviewer URL", user: "u", first: "p", last: "q",
	})
}

func TestTwoPasswordCaretMappingCoversLengthsRunesAndInsertions(t *testing.T) {
	tests := []twoPasswordCredentials{
		{name: "different lengths", user: "u", first: "abcdefghijklmnop", last: "xy"},
		{name: "multibyte", user: "χρήστης", first: "κωδικός", last: "μυστικό"},
		{name: "empty first password", user: "u", first: "", last: "q"},
		{name: "empty final password", user: "u", first: "p", last: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testTwoPasswordCaretPositions(t, tc)
		})
	}
}

func testTwoPasswordCaretPositions(t *testing.T, credentials twoPasswordCredentials) {
	t.Helper()
	const linePrefix = "database: {url: "
	url := "postgres://" + credentials.user + ":" + credentials.first +
		"@h/x?sslpassword=" + credentials.last
	line := linePrefix + url + "}"
	urlColumn := runeCount(linePrefix) + firstColumn
	firstFrom := urlColumn + runeCount("postgres://"+credentials.user+":")
	firstPast := firstFrom + runeCount(credentials.first)
	lastFrom := firstPast + runeCount("@h/x?sslpassword=")
	lastPast := lastFrom + runeCount(credentials.last)
	firstShift := runeCount(redactionPlaceholder) - runeCount(credentials.first)
	lastShift := runeCount(redactionPlaceholder) - runeCount(credentials.last)

	positions := []twoPasswordCaretPosition{
		{name: "before both", source: urlColumn + runeCount("postgres://"),
			rendered: urlColumn + runeCount("postgres://"), target: []rune(credentials.user)[0]},
		{name: "between spans", source: firstPast,
			rendered: firstPast + firstShift, target: '@'},
		{name: "after both", source: lastPast,
			rendered: lastPast + firstShift + lastShift, target: '}'},
	}
	positions = append(positions,
		passwordCaretPosition("inside first", firstFrom, credentials.first, 0, '@'),
		passwordCaretPosition("inside final", lastFrom, credentials.last, firstShift, '}'),
	)
	for _, position := range positions {
		assertTwoPasswordCaret(t, line+"\n", position)
	}
}

type twoPasswordCaretPosition struct {
	name             string
	source, rendered int
	target           rune
}

func passwordCaretPosition(
	name string,
	from int,
	password string,
	priorShift int,
	next rune,
) twoPasswordCaretPosition {
	if password == "" {
		return twoPasswordCaretPosition{
			name: name + " insertion", source: from,
			rendered: from + priorShift + runeCount(redactionPlaceholder), target: next,
		}
	}
	return twoPasswordCaretPosition{
		name: name, source: from + runeCount(password) - 1,
		rendered: from + priorShift, target: '[',
	}
}

func assertTwoPasswordCaret(t *testing.T, source string, position twoPasswordCaretPosition) {
	t.Helper()
	t.Run(position.name, func(t *testing.T) {
		diagnostic := Error{File: "url.yaml", Line: 1, Col: position.source, Msg: "message"}
		block := Errors{diagnostic}.Render([]byte(source))
		header := fmt.Sprintf("url.yaml:1:%d\n", position.source)
		if !strings.HasPrefix(block, header) {
			t.Errorf("header does not retain source coordinate %q:\n%s", header, block)
		}
		wantCaret := newSnippetBlock(1).caretColumn(position.rendered)
		if got := caretColumn(t, block); got != wantCaret {
			t.Errorf("caret column = %d, want %d across two password edits:\n%s",
				got, wantCaret, block)
		}
		if got := caretTarget(t, block); got != position.target {
			t.Errorf("caret target = %q, want %q:\n%s", got, position.target, block)
		}
	})
}

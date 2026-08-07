package config

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

// This file reaches the redactor's three unreachable decisions by constructing their state directly.
//
// Each exists because the component's contract is to over-redact rather than to guess, and no
// document reaches any of them today -- which is exactly why none of them was asserted. A branch no
// input reaches is a branch no input pins either: two of the three were measured as fail-*open*
// during review, and mutating the third to keep its line survived the entire suite.

// TestKeptVerbatimRefusesATokenItCannotPositivelyClassify reaches keptVerbatim's default arm.
// redactedOutsideAnyBlock answers aKeyThisFileCannotRead itself before it calls keptVerbatim, so no
// document arrives here.
func TestKeptVerbatimRefusesATokenItCannotPositivelyClassify(t *testing.T) {
	// Every enclosing state, because the arm is asked for one of them and must answer the same for
	// all: a name only a parser can decode may be a sensitive one wherever the line sits.
	for _, blockIndent := range []int{outsideSensitiveBlock, 0, 4} {
		if keptVerbatim(aKeyThisFileCannotRead, blockIndent) {
			t.Errorf("keptVerbatim keeps a key it cannot read with blockIndent %d; the name it "+
				"decodes to may be a sensitive one, so the line may only be blanked", blockIndent)
		}

		// A kind added after this test was written lands on the same arm, which is the reason the
		// arm is a default rather than a case.
		const addedLater leadingToken = 200
		if keptVerbatim(addedLater, blockIndent) {
			t.Errorf("keptVerbatim keeps an unclassified token kind with blockIndent %d; the safe "+
				"side to default to is the one that hides it", blockIndent)
		}
	}
}

// TestKeptVerbatimKeepsTheTwoShapesItCanPositivelyClassify is the other side of that default. A
// keptVerbatim that refused everything would pass the test above while blanking the line every
// diagnostic about a plain-scalar document consists of.
func TestKeptVerbatimKeepsTheTwoShapesItCanPositivelyClassify(t *testing.T) {
	if !keptVerbatim(aKeyThisFileCanRead, outsideSensitiveBlock) {
		t.Error("keptVerbatim blanks a key whose name is exactly what is written and which no " +
			"sensitive name matched, so nothing on that line is governed by one")
	}
	if !keptVerbatim(noKeyAtAll, outsideSensitiveBlock) {
		t.Error("keptVerbatim blanks a line that is no key at all with no sensitive block open, " +
			"which protects nothing and costs a diagnostic its own text")
	}
	if keptVerbatim(noKeyAtAll, 0) {
		t.Error("keptVerbatim keeps a line that is no key at all while a sensitive block is open; " +
			"below such a block it is that key's value written at an illegal indentation")
	}
}

// TestRedactingAValueWithNoColumnBlanksItsWholeLine reaches redactValue's answer for a value that
// has a line and no column. positionOf reports firstColumn or more for every node a document
// produces, so this state is constructed rather than parsed -- and it used to return the line
// untouched, which is a fail-open answer inside the component whose whole contract is the opposite.
func TestRedactingAValueWithNoColumnBlanksItsWholeLine(t *testing.T) {
	const written = "secrets: PLAINTEXT"
	text := newSource("listeners.yaml", []byte(written+"\n"))

	rewritten, spansLines, edits := redactValue(written, text, sensitiveValue{
		line: firstLine, column: noColumn, text: "PLAINTEXT", kind: entireValue,
	})

	if strings.Contains(rewritten, "PLAINTEXT") {
		t.Errorf("redactValue returned %q for a value it could not place on its line; the line goes "+
			"whole rather than not at all", rewritten)
	}
	if rewritten != redactionPlaceholder {
		t.Errorf("redactValue returned %q, want the whole line replaced by %q",
			rewritten, redactionPlaceholder)
	}
	if !spansLines {
		t.Error("redactValue reported the value does not span lines; where it reaches is exactly " +
			"what could not be read, so the lines beneath it must be blanked too")
	}
	if edits != nil {
		t.Errorf("redactValue reported %v column shifts for a replacement whose extent is unknown",
			edits)
	}
}

// TestLocateRefusesANodeThisTextDidNotProduce reaches locate's other refusal. A node carrying no
// position has no line, and a value with no line is one the walk must drop rather than report at
// line zero -- which would send a replacement to whichever line the copy holds first.
//
// The node is built rather than parsed, because every node this text produces carries a position.
// Its token is real and its position is the one a token that came from nowhere has.
func TestLocateRefusesANodeThisTextDidNotProduce(t *testing.T) {
	text := newSource("listeners.yaml", []byte("secrets: PLAINTEXT\n"))
	fromNowhere := ast.String(&token.Token{Type: token.StringType, Value: "PLAINTEXT"})

	located, positioned := locate(text, fromNowhere, entireValue)

	if positioned {
		t.Fatalf("locate placed a node carrying no position at line %d column %d; this text did "+
			"not produce it, so there is no line holding it to replace", located.line, located.column)
	}
	if located != (sensitiveValue{}) {
		t.Errorf("locate returned %+v alongside its refusal; a caller reading the value past a "+
			"false would be reading a position nothing derived", located)
	}
}

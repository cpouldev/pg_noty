package config

import (
	"regexp"
	"strings"
)

// This file is the pinned `${…}` grammar as it is *written*: what an author may spell as a
// reference, and what this stage says about a spelling it will not read as one. What a
// reference stands for once the environment has been consulted is envexpansion.go's question,
// and where in a document one may appear at all is interpolate.go's, so no file here knows
// another's rule.
//
// The grammar is closed over its input by construction: every `${` in a scalar is either the
// tail of the escape `$${` or the start of an occurrence that is resolved or refused, and the
// scan has no third arm. That it is closed over what it *emits* as well is a separate
// question, answered per byte source in envexpansion.go.

// The grammar's literals, spelled once each. The escape is written from the opening
// delimiter rather than as its own three characters, so the two cannot drift apart, and
// variableNamePattern is quoted by the refusal below so a diagnostic cannot name a rule the
// matcher does not apply.
const (
	referenceOpen       = "${"
	referenceClose      = "}"
	escapeSentinel      = "$" + referenceOpen
	defaultMarker       = ":-"
	variableNamePattern = `[A-Za-z_][A-Za-z0-9_]*`
)

var variableName = regexp.MustCompile(`^` + variableNamePattern + `$`)

// What this stage says about an occurrence it will not read as a reference. None of these
// quotes the document's own text, deliberately: a message is rendered exactly as it is built
// while only the quoted source *line* passes through the redactor, so a message carrying a
// scalar's text would be the one path by which a literal secret reaches output un-redacted
// (ADR-5). The caret says which value; the message says what is wrong.
const (
	emptyNameRefusal    = "malformed environment reference: a variable name is required"
	badNameRefusal      = "malformed environment reference: a variable name must match " + variableNamePattern
	unterminatedRefusal = "malformed environment reference: no closing brace"

	// escapeHint is the remedy every malformed occurrence shares: the author may have meant
	// the two characters, and there is one way to write those.
	escapeHint = "write $${ to mean a literal ${"
)

// fault is one occurrence the grammar refuses, carried as the two halves of the diagnostic
// it becomes. An empty message is what "there is no fault" means, so no caller holds a
// pointer to decide.
type fault struct {
	message string
	hint    string
}

// reference is one `${…}` occurrence as the grammar reads it.
type reference struct {
	// width is how many bytes of the scalar the occurrence spans. The scan resumes after it
	// whether or not it was legal, so one mistake does not cascade over the rest of a scalar.
	width int
	// name is the variable named, and is empty whenever refusal is not.
	name string
	// fallback is the text written after `:-`, and hasDefault is what keeps `${N:-}` -- an
	// explicitly empty default, the deliberate way to say "empty is what I mean" -- apart from
	// `${N}`, which supplies none.
	fallback   string
	hasDefault bool
	// refusal states why the grammar will not read this occurrence as a reference at all, and
	// is empty when it does.
	refusal string
}

// readReference reads the occurrence beginning at the start of text, which the caller has
// already established opens with the delimiter.
func readReference(text string) reference {
	if len(text) < len(referenceOpen) {
		// Too short to hold the delimiter, so nothing here opens an occurrence and nothing
		// closes one. Both callers today check the prefix first; a third that does not gets
		// the diagnostic every unclosed occurrence gets rather than an index panic inside the
		// loader, which is the same fail-closed answer the rest of this grammar gives.
		return reference{width: len(text), refusal: unterminatedRefusal}
	}

	inside := text[len(referenceOpen):]

	closed := strings.Index(inside, referenceClose)
	if closed < 0 {
		// Nothing closes it, so the occurrence runs to the end of the scalar and nothing is
		// left after it to scan.
		return reference{width: len(text), refusal: unterminatedRefusal}
	}

	// The first `}` ends the occurrence, and that one rule is both of the grammar's bounds: a
	// default may not contain `}`, and references do not nest -- `${${A}}` reads `${A` as a
	// name, refuses it, and leaves the trailing `}` as the ordinary text it is.
	width := len(referenceOpen) + closed + len(referenceClose)
	name, fallback, hasDefault := strings.Cut(inside[:closed], defaultMarker)

	if refusal := nameRefusal(name); refusal != "" {
		return reference{width: width, refusal: refusal}
	}
	return reference{width: width, name: name, fallback: fallback, hasDefault: hasDefault}
}

// nameRefusal states why name is not a variable name, and is empty when it is one.
func nameRefusal(name string) string {
	if name == "" {
		return emptyNameRefusal
	}
	if !variableName.MatchString(name) {
		return badNameRefusal
	}
	return ""
}

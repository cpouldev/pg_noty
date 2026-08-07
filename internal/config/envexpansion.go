package config

import (
	"fmt"
	"strings"
)

// This file is what one scalar's text becomes: the left-to-right scan, what each occurrence
// the grammar read (envreference.go) stands for once the environment has been consulted, and
// which of the bytes that produces are owed the grammar a second time.
//
// That last question is the one this file exists to answer in a single place. A substituted
// value has two possible byte sources -- the environment, and the document itself -- and only
// one of them has earned the opacity that makes re-scanning unsafe. Where in a document any of
// this may happen at all is interpolate.go's question.

// expansion is what the grammar makes of one scalar's text.
type expansion struct {
	// text is the substituted result. It is left empty whenever faults is not, because a
	// scalar the grammar refused keeps the text it was written with: a caller that read this
	// without checking gets nothing rather than a half-substituted value.
	text string
	// faults holds one entry per occurrence refused, in the order they appear, so a scalar
	// carrying two mistakes reports both rather than only the first.
	faults []fault
	// fromEnvironment reports whether any of these bytes came from the environment, which is
	// what tells a value the environment supplied apart from one the document did. It is set
	// where env() answered and nowhere else: neither the escape nor an author-written default
	// makes a value environment-sourced, and W1 has to see both as the literals they are.
	fromEnvironment bool
}

// expand applies the grammar to one scalar's text, left to right.
//
// The scan continues past a refused occurrence rather than abandoning the scalar, so a value
// holding two unset variables names both -- the per-occurrence reporting AC #7 requires, at
// the granularity a single scalar can offer.
func expand(text string, env EnvLookup) expansion {
	var (
		built  strings.Builder
		result expansion
	)

	for at := 0; at < len(text); {
		rest := text[at:]

		if strings.HasPrefix(rest, escapeSentinel) {
			// The one escape. What it writes is not scanned again, so `$${NAME}` yields the
			// seven characters `${NAME}` rather than a reference to NAME.
			built.WriteString(referenceOpen)
			at += len(escapeSentinel)
			continue
		}
		if !strings.HasPrefix(rest, referenceOpen) {
			// A lone `$`, a bare `$$` and every byte that is not a `$` are literal.
			built.WriteByte(text[at])
			at++
			continue
		}

		found := readReference(rest)
		at += found.width

		contributed := found.expanded(env)
		built.WriteString(contributed.text)
		result.faults = append(result.faults, contributed.faults...)
		result.fromEnvironment = result.fromEnvironment || contributed.fromEnvironment
	}

	if len(result.faults) != 0 {
		return result
	}
	result.text = built.String()
	return result
}

// resolution is what a legal reference stands for, together with which of the two byte
// sources supplied it. Keeping the sources apart is the whole of the closure rule below:
// nothing else about the two strings differs, so a single untyped return would hand the
// author's own text the exemption only external bytes have earned.
type resolution struct {
	text            string
	fromEnvironment bool
}

// expanded is what one occurrence contributes to the scalar it was written in.
//
// This is where the grammar closes over what it emits, and the answer differs by source.
// Bytes the environment supplied stay opaque: re-scanning them is the injection this stage
// exists to prevent, so a value holding `${` is written exactly as it was read (ADR-5). A
// default is text the author wrote in this document, so it is owed the same grammar as every
// other byte of the document, and `${A:-${B}}` refuses the `${B` it holds rather than
// emitting it as a literal.
//
// Re-entry terminates: a default is a strict substring of the occurrence carrying it, which
// is at least the delimiters longer.
func (r reference) expanded(env EnvLookup) expansion {
	resolved, refused := r.substitution(env)
	if refused.message != "" {
		return expansion{faults: []fault{refused}}
	}
	if resolved.fromEnvironment {
		return expansion{text: resolved.text, fromEnvironment: true}
	}
	return expand(resolved.text, env)
}

// substitution is what the reference stands for and where its bytes came from, or the fault
// saying why it stands for nothing; an empty fault message decides which of the two is
// meaningful. All three environment states the seam reports are kept apart here -- set to a
// value, set to the empty string, not set at all -- because collapsing any two changes what a
// configuration means.
func (r reference) substitution(env EnvLookup) (resolution, fault) {
	if r.refusal != "" {
		return resolution{}, fault{message: r.refusal, hint: escapeHint}
	}

	value, set := env(r.name)

	// A default stands in for both the states that mean "there is no text here": unset, and
	// set to the empty string. That is the grammar's own wording, and it is why the
	// environment seam has to report three states rather than two.
	if r.hasDefault && value == "" {
		return resolution{text: r.fallback}, fault{}
	}
	if !set {
		// Without a default, an unset variable is the mistake this diagnostic exists for and is
		// never silently the empty string. Set to the empty string is a different answer: it was
		// set deliberately, and field-level validation refuses an empty value where one matters.
		return resolution{}, r.unresolved()
	}
	return resolution{text: value, fromEnvironment: true}, fault{}
}

// unresolved is the diagnostic for a reference whose variable is not set. It names the
// variable, safe to quote where a scalar's text is not: the name has already matched the
// pinned charset, so it holds nothing but letters, digits and underscores.
func (r reference) unresolved() fault {
	return fault{
		message: fmt.Sprintf("environment variable %q is not set", r.name),
		hint:    fmt.Sprintf("set %s, or write ${%s:-default} to supply a fallback", r.name, r.name),
	}
}

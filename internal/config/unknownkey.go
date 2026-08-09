package config

import (
	"strconv"

	"github.com/goccy/go-yaml/ast"
)

// This file is R41 -- no unknown keys at any nesting level -- and the two hints that can accompany
// it: the closest declared name, and, for a key the corrected contract moved, where it went.
//
// Which names a level declares and which it has moved are schema.go's, and how close is close
// enough is suggest.go's. What is decided here is the order the two are asked in, which one case
// makes load-bearing rather than incidental.

// unknownField names a key the level it is written at does not declare. The wording is the
// Diagnostic rendering convention's own, transcribed from the authoritative block rather than
// chosen here.
func unknownField(name string) string { return `unknown field ` + strconv.Quote(name) }

// didYouMean is the suggestion's wording, likewise from that block.
func didYouMean(candidate string) string { return `did you mean ` + strconv.Quote(candidate) + `?` }

// keyAsWritten is the name a diagnostic calls a key by: the text the key holds, or -- for a key
// that holds none -- the source text its author typed.
//
// The second case is the whole reason this exists. keyTextOf answers ("", true) for every key the
// parser read as something other than text: `16:` is the integer sixteen and carries no name, and
// so do `true:`, `~:` and the merge indicator. Quoting that answer renders `unknown field ""`,
// which leaves an author a rule and a line and no key at all.
//
// The two are told apart by whether the key holds text rather than by whether the text is empty,
// because `"": 1` is a field genuinely named "" and `unknown field ""` is what finds it. For every
// key that does hold text this answers exactly what keyTextOf does, so the contract lookup and the
// message can never come to name different keys.
//
// The rendering is displayed and compared with nothing. Which two keys are one key stays
// keyIdentity's, for the reason Implementation Note 6 records: a rendering splits one key into two,
// and splitting is the unsafe direction there.
func keyAsWritten(key ast.Node) string {
	beneath := beneathKeyProperties(key)
	if text, holdsText := scalarTextOf(beneath); holdsText {
		return text.written()
	}

	at := beneath.GetToken()
	if at == nil {
		// A node the parser did not build carries no source text to quote, which is the same
		// absence tokenPosition answers for. No document reaches it -- every key this walk meets
		// was parsed -- and TestAKeyWithNoTokenIsNamedByNothingRatherThanPanicking reaches it by
		// construction.
		return ""
	}
	return at.Value
}

// refuseAnUndeclaredName reports one name the level does not declare, called as its author wrote it.
//
// A key the contract *moved* is named by its migration hint rather than by the nearest declared
// name, and the order is load-bearing rather than incidental: `secret` is one edit from `secrets`,
// so asking for a suggestion first would answer `did you mean "secrets"?` -- correct about the
// spelling and silent about the fact that the key is now a list (AC #10).
//
// Otherwise the closest declared name within the threshold is offered, and when nothing is close
// enough the level's own words are, which is how the operations level names the three statements
// it accepts instead of leaving an author with no direction at all.
func (p *shapePass) refuseAnUndeclaredName(level mappingLevel, key ast.Node, written string) {
	refusal := level.refusalOfAnUndeclaredName()
	why := fault{message: unknownField(written), hint: refusal.hint}

	if moved, wasRemoved := level.removedKey(written); wasRemoved {
		why.hint = moved
	} else if closest, found := closestName(written, level.declaredNames()); found {
		why.hint = didYouMean(closest)
	}

	p.reportAs(refusal.rule, key, why)
}

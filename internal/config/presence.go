package config

import "github.com/goccy/go-yaml/ast"

// This file is what every decoded value carries: whether the configuration file wrote it, where it
// is written, whether the wrapper could read it, and what it could not read. The wrappers themselves
// are scalar.go's and raw.go's; this is the state all of them share.
//
// CONFORMANCE with skill Pattern 6, on the naming. Pattern 6's example type carries two flags called
// `Present` and `Valid`; this package calls the first one **Set** and keeps the second, so a reader
// arriving from the skill can map the two. `Set` is a field because a file either wrote the key or
// did not, and `Valid` is derived, because "the wrapper read a value" is the absence of a recorded
// refusal and two stored flags could disagree about it with nothing failing.
//
// **Three states, and Step 10's merge depends on all three.** Absence is Set == false and is what
// lets the merge inherit; a written value is Set == true and Valid(), and is what lets the merge
// override -- including with a value equal to Go's zero, which is the whole point:
// `jitter: false` and `max_attempts: 0` are decisions an author wrote down, and a merge reading the
// value alone would substitute the built-in default over both of them (AC #22). The third state,
// Set == true and not Valid(), is a value the author wrote and the wrapper could not read; it is what
// stops stage H reporting a constraint violation about a zero this package invented, and what stops
// stage I merging one in (the no-double-report rule).
//
// A position cannot be materialised here, because the decoder hands a wrapper its node and nothing
// else: a column is derived against the raw source line (ADR-4) and the source is not reachable from
// a node. Stage G therefore resolves every wrapper it decoded (resolvewalk.go), which is also when a
// recorded refusal becomes a diagnostic.

// refusal is one thing a wrapper could not read, and the node the diagnostic about it anchors on.
//
// The node is carried per refusal rather than taken from the wrapper, because a list refuses its
// *elements*: a diagnostic about one of them anchors on that element (ADR-6's element class), not on
// the list holding it.
type refusal struct {
	at  ast.Node
	why fault
}

// presence is the state every wrapper shares, embedded by all of them.
type presence struct {
	// Set reports whether the configuration file wrote this key. It is false for an absent key, and
	// true for every key an author wrote -- including one whose value the wrapper could not read.
	//
	// A key written with no value after the colon is the one case that reads as absent, and that is
	// the library's decision rather than this package's: measured on v1.19.2, the decoder does not
	// call a NodeUnmarshaler at all for a null value, so nothing here is reached to set the flag
	// (TestGoccySkipsANodeUnmarshalerForANullValue, TestAKeyWrittenWithNoValueDoesNotReachItsWrapper).
	Set bool

	// node is the value the decoder handed this wrapper, kept because a position is derived from it
	// once stage G supplies the source. at is that position, and is nil until then.
	node ast.Node
	at   Positioned

	// refused is everything this wrapper could not read, in the order it met them, so a list holding
	// two unusable elements names both in one run.
	refused []refusal
}

// began records that the file wrote this key, and the node its position is derived from.
//
// Every wrapper's UnmarshalYAML calls it first, before anything can go wrong, which is what makes
// "the author wrote this" independent of whether the value could be read.
func (p *presence) began(node ast.Node) {
	p.Set, p.node = true, node
}

// refuse records that this wrapper could not read its own value.
func (p *presence) refuse(why fault) {
	p.refuseAt(p.node, why)
}

// refuseAt records that this wrapper could not read something written at another node -- an element
// of a list, an entry of a mapping -- so the diagnostic anchors where the author has to look.
func (p *presence) refuseAt(node ast.Node, why fault) {
	p.refused = append(p.refused, refusal{at: node, why: why})
}

// Valid reports whether the wrapper read a value. It is false for an absent key and for a value the
// wrapper could not read, which are the two states a rule has nothing to judge in.
func (p presence) Valid() bool {
	return p.Set && len(p.refused) == 0
}

// wasWritten reports whether the file wrote this key, for the one caller that cannot read the field: an
// interface can carry a method and not a field.
//
// Not `written`, which valueposition.go's scalarText already uses for the *text* a scalar holds. Two
// methods of one name answering a bool and a string is a reader's problem rather than the compiler's.
func (p presence) wasWritten() bool { return p.Set }

// resolve materialises this value's position against the source and reports every refusal it
// recorded, under the rule the pass itself raises. It is stage G's second half, and the only place a
// wrapper meets the source.
func (p *presence) resolve(pass *decodePass) {
	p.resolveAs(pass, pass.rule)
}

// resolveAs is the same for the one wrapper whose refusal the contract numbers: a duration this package
// cannot read is R40 wherever it is written, because the rule is stated over every duration value rather
// than over a key (scalarlist.go's sibling case does not arise -- a list refuses a *shape*, which the
// contract numbers nowhere).
//
// The rule is an argument rather than a field on the wrapper because there is then one declaration of what
// the stage raises -- the pass's, as stages D, E and F declare theirs -- with the one exception named at
// the wrapper it belongs to.
func (p *presence) resolveAs(pass *decodePass, reportedBy RuleID) {
	p.at = positionOf(pass.src, p.node)

	for _, refused := range p.refused {
		pass.reportAs(reportedBy, refused.at, refused.why)
	}
}

// resolveSemanticAs reports a numbered scalar-type rule through ADR-6's anchor table. It is separate
// from resolveAs because structural conversion failures remain stage G diagnostics.
func (p *presence) resolveSemanticAs(pass *decodePass, reportedBy RuleID) {
	p.at = positionOf(pass.src, p.node)
	for _, refused := range p.refused {
		reportSemantic(&pass.stage, reportedBy, anchorSet{
			value: positionOf(pass.src, refused.at),
		}, refused.why)
	}
}

// The four Positioned answers, promoted to every wrapper, so a stage-H rule reads a position without
// importing the parser (ADR-2). They are derived from the resolved position rather than stored
// separately, so no wrapper can carry a line from one node and a column from another.

func (p presence) File() string { return p.where().File() }
func (p presence) Line() int    { return p.where().Line() }
func (p presence) Col() int     { return p.where().Col() }
func (p presence) Path() string { return p.where().Path() }

// where is this value's position, and answers a positionless one for a wrapper stage G has not
// resolved. That is not a hedge against resolution being skipped: a wrapper the decoder never filled
// has no node, so there is no position to answer, and a zero sourcePos is what "nothing to point at"
// already means everywhere else in the package (position.go).
func (p presence) where() Positioned {
	if p.at == nil {
		return sourcePos{}
	}
	return p.at
}

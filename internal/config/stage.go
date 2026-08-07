package config

import "github.com/goccy/go-yaml/ast"

// This file is what a pass of the stage machine accumulates: the source its diagnostics are
// positioned against, the rule they are tagged with, and the collection they are added to.
//
// It is shared because the alternative is nine copies. Stages D and E each began with their own
// one-line accumulator differing only in the rule and in how the two halves of the message
// arrived, and seven stages are still to be written -- so the second copy is where the shape gets
// settled rather than where it gets repeated.
//
// The shape is the one stage E arrived at: a refusal is a named `fault` declared beside the code
// that raises it, and this is where a fault becomes a positioned diagnostic.

// stage is one pass's accumulator, embedded by the pass that runs it.
type stage struct {
	src *source
	// rule is what every diagnostic this pass raises is tagged with. It is a field rather than an
	// argument because it is a property of the pass rather than of the occurrence: a pass cannot
	// raise another stage's rule by mistake if it never names one.
	rule  RuleID
	diags Errors
}

// report adds one diagnostic tagged with the pass's own rule, anchored on the node the condition
// is about.
func (s *stage) report(at ast.Node, why fault) {
	s.reportAs(s.rule, at, why)
}

// reportAs adds one diagnostic tagged with a rule of its own.
//
// Stages D and E raise one rule each and never call this directly. Stage F does: the structural
// pass owns eleven of the contract's numbered rules, so there the rule is a property of the
// condition rather than of the pass, and only the pass's own structural refusal -- a shape the
// stages after it cannot read -- takes the field.
//
// The position comes from position.go, this package's only column derivation (ADR-4), so a stage
// added later inherits the derivation rather than writing a second one.
func (s *stage) reportAs(rule RuleID, at ast.Node, why fault) {
	s.diags = append(s.diags, NewError(rule, positionOf(s.src, at), why.message).WithHint(why.hint))
}

// reportPositionedAs is the L1 counterpart for stages whose wrappers already carry positions.
// Keeping accumulation here means semantic validation reuses the stage mechanism without importing
// the parser merely to turn a position back into a node.
func (s *stage) reportPositionedAs(rule RuleID, at Positioned, why fault) {
	s.diags = append(s.diags, NewError(rule, at, why.message).WithHint(why.hint))
}

// reportAboutMapping adds one diagnostic about a mapping as a whole, anchored on that mapping's
// first key.
//
// It is the second of ADR-6's anchor classes, and it lives here beside the first so that a stage
// picks a class rather than inventing an anchor. A mapping's own token is the colon of its first
// entry -- measured -- so anchoring a mapping-level diagnostic on it would put every such caret on
// punctuation; positionOfMapping is where that reasoning is written down.
func (s *stage) reportAboutMapping(rule RuleID, mapping ast.Node, why fault) {
	s.diags = append(s.diags, NewError(rule, positionOfMapping(s.src, mapping), why.message).WithHint(why.hint))
}

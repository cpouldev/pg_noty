package config

import (
	"strconv"

	"github.com/goccy/go-yaml/ast"
)

// This file is stage F: it judges the document's *shape* against the contract, and judges no
// value. Which keys exist where, which required ones do not, which values have the wrong YAML
// shape, and which names one mapping holds twice -- and nothing about what any of them says, which
// is stages H and I's subject and would double-report if it were also this one's.
//
// CONFORMANCE with skill Pattern 3 (unknown-key detection). The walk is hand-rolled and driven by
// the declared shape in schema.go, per-node-shape, and it runs after merge-key resolution so a
// merged-in key is not misreported -- which are the pattern's own prescriptions. One thing the
// pattern leaves available is refused outright: it offers `yaml.Strict()` /
// `DisallowUnknownField()` as defence-in-depth beside the hand-rolled walk, and ADR-1 excludes
// every strictness option from this package. V4 measured why: strict decode returns at the *first*
// unknown field, which AC #8 rules out, and it is blind inside a NodeUnmarshaler subtree, so it
// would give false assurance about exactly the levels -- operations, headers -- this walk exists to
// reach. TestNoStrictnessOptionIsPassedAnywhere holds the exclusion.
//
// Every anchor it uses is one of ADR-6's classes: the key's own token for a key that is wrong, the
// value's own token for a container that is empty or of the wrong shape, and the enclosing scope's
// first key for a key that was never written at all (requiredkeys.go).

// shapePass is what one stage-F run carries: what every stage accumulates (stage.go), and whether
// the shape the document has left is one the stages after it can read.
type shapePass struct {
	stage
	// undecodable is set by the one class of finding that stops the run. It is a field rather than
	// an early return because the policy is to accumulate: a document with a mis-shaped `worker`
	// block still has every other mistake reported in the same run, and only then does the run end.
	undecodable bool
}

// checkShape runs stage F over the document rooted at root.
//
// It reports every unknown key, every missing required key and every value whose shape the
// contract does not admit -- all of them, in one pass -- and says whether what is left is a
// document the stages after it can read. Unknown and missing keys leave it readable: decode can
// skip a key it does not know and a rule can fire on a value that is absent, so blocking the later
// stages on either would be exactly the edit-and-rerun loop AC #12 exists to remove. A value of the
// wrong shape does not: there is nothing to decode a mapping into where a scalar was declared, and
// a mapping holding one key twice fails the decoder outright.
func checkShape(src *source, root ast.Node) (diags Errors, decodable bool) {
	pass := newShapePass(src)

	pass.checkMapping(schemaLevels[levelRoot], root, "")

	return pass.diags, !pass.undecodable
}

// newShapePass is one stage-F run over src, with its diagnostics tagged by default with the one
// rule this stage owns that the contract does not number. Every numbered rule it raises is named
// at the condition that raises it.
func newShapePass(src *source) *shapePass {
	return &shapePass{stage: stage{src: src, rule: RuleShape}}
}

// What this stage refuses, as the faults it raises here; the rest are declared beside the checks
// that raise them, in unknownkey.go, valueshape.go, requiredkeys.go and keyrepeats.go.
//
// None of them quotes a *value*, for the reason every other stage's refusals record: a message is
// rendered exactly as it is built, so a message carrying a value's text would be a path by which a
// secret reaches output un-redacted (ADR-5). A key's text is quoted, because a key is never
// interpolated and naming it is the whole point of an unknown-key diagnostic.

// everyEntryMustHold is the same for the elements of a list of mappings, where the list itself has
// the shape the contract declares and one of the things in it does not.
func everyEntryMustHold(name string, kinds nodeKinds) fault {
	return fault{message: "each " + strconv.Quote(name) + " entry must be " + namesOfKinds(kinds)}
}

// legalOnlyUnder is what a key declared by a shared level but permitted beneath one of the keys
// that reach it is told. The message states the rule and needs no hint beside it: there is one
// place the key belongs and it is named.
func legalOnlyUnder(name, sibling string) fault {
	return fault{message: strconv.Quote(name) + " is legal only under " + strconv.Quote(sibling)}
}

// unreadableKeyShape is this stage's answer to a key it cannot read. It shares stage D's wording
// for the condition, because it is the same condition, and states this stage's own reason for
// needing to read one.
//
// No document reaches it: stage D refuses every unreadable key an author wrote and the run stops
// there, and stage E refuses every key the fold invents. It is reached instead by
// TestAKeyThisStageCannotReadIsRefusedRatherThanWalkedPast, because a fail-closed branch that
// nothing asserts is a branch that can be deleted with the suite still green.
var unreadableKeyShape = fault{
	message: unrecognisedKeyShapeMessage,
	hint:    "a key has to be readable before it can be matched against the contract; write it as a scalar",
}

// checkMapping judges one mapping against the level the contract declares for it: the names it
// holds twice, every key it writes, and -- when this level is one of the two the contract anchors
// missing keys on -- every required key it does not.
//
// reachedThrough is the name of the key whose value this mapping is, which one rule needs and the
// rest ignore: a shape reached from several keys can permit one of its own keys beneath only one
// of them (R29). The document root is reached through no key and names none.
func (p *shapePass) checkMapping(level mappingLevel, node ast.Node, reachedThrough string) {
	mapping, isMapping := beneathNodeProperties(node).(*ast.MappingNode)
	if !isMapping {
		p.report(node, everyEntryMustHold(reachedThrough, mappingValue))
		p.undecodable = true
		return
	}

	// Both ways one mapping can name one thing twice read the entries in the order they were
	// written, so the ordering is derived once here rather than once per equivalence (keyrepeats.go).
	ordered := entriesInWrittenOrder(p.src, mapping)

	p.refuseARepeatedName(ordered)
	if level.httpFieldNames {
		p.refuseNamesDifferingOnlyInCase(ordered)
	}

	for _, entry := range mapping.Values {
		p.checkEntry(level, entry, reachedThrough)
	}
	p.reportMissingRequiredKeys(level, mapping)
}

// checkEntry judges one key of one mapping and, when the contract declares a shape beneath it,
// everything that shape holds.
//
// Each arm returns rather than falling through to the next, which is what keeps one authoring
// mistake to one diagnostic: a key the contract never heard of has no declared shape to check, and
// a key written in the wrong place is not also judged on its contents -- telling an author to fill
// in a list they have to delete is two instructions for one edit.
func (p *shapePass) checkEntry(level mappingLevel, entry *ast.MappingValueNode, reachedThrough string) {
	name, readable := keyTextOf(entry.Key)
	if !readable {
		p.report(entry.Key, unreadableKeyShape)
		p.undecodable = true
		return
	}

	if level.freeForm {
		// Every name is legal here and the contract declares no shape beneath any of them, so
		// there is nothing more this entry can be asked. What is still asked of a header mapping
		// is asked of it as a whole, because it takes two names to break: R20 above, and R19 and
		// R21 in Step 9.
		return
	}

	spec, declared := level.key(name)
	if !declared {
		// Matched against the contract by the text the key holds, and named in the diagnostic by
		// the text its author typed. The two differ for exactly one class -- a key the parser read
		// as a number, a boolean or nothing at all, which holds no text and so matches no declared
		// name -- and the diagnostic has to name it or it names nothing (unknownkey.go).
		p.refuseAnUndeclaredName(level, entry.Key, keyAsWritten(entry.Key))
		return
	}
	if spec.legalUnder != "" && spec.legalUnder != reachedThrough {
		p.reportAs(spec.legality, entry.Key, legalOnlyUnder(name, spec.legalUnder))
		return
	}

	p.checkValue(spec, entry.Value)
}

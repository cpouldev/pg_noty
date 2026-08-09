package config

import "github.com/goccy/go-yaml/ast"

// This file is stage D: it substitutes `${…}` into the document's scalar *values*, and
// nothing else. The grammar it applies is written in envreference.go and evaluated in
// envexpansion.go; what this file owns is where that grammar may be applied at all, which is
// a question about the shape of the tree rather than about the shape of a scalar.
//
// Substituting into parsed nodes rather than into raw text is the whole reason the stage
// sits here. A value holding a colon, a `#` or a newline would change the document's shape
// and destroy every position the diagnostics depend on if it were spliced into the text
// before parsing; substituted into a node it is opaque bytes that no one parses again.
//
// The source is never touched. Every rendered snippet is read from those bytes, so what a
// diagnostic shows is `${SIGNING_SECRET}` and never what it resolved to (ADR-5).

// What this stage says about a value whose shape it cannot descend. It quotes no text of the
// document, for the reason envreference.go's refusals record. The key position's two answers are
// keyreference.go's, because stage E asks that question too.
const (
	unrecognisedShapeMessage = "unsupported YAML shape in a value position"
	unrecognisedShapeHint    = "interpolation reaches scalars, sequences and mappings; write the value as one of those"
)

var unrecognisedValueShape = fault{message: unrecognisedShapeMessage, hint: unrecognisedShapeHint}

// originalTexts records, for each value the environment supplied bytes to, the text that was
// written in the configuration file. W1 (Step 11) is its reader: it asks whether a signing
// secret is a literal committed to the repository or a reference to the environment, and
// this map is the only thing that still knows.
//
// Membership answers where the bytes came from, not which syntax was written, so a value
// resolved entirely from an author's default is absent: `${SECRET:-dev-secret}` with the
// variable unset is a secret committed to the repository, and W1 has to warn about it.
//
// It is keyed by the node a diagnostic about the value would anchor on, which is the node a
// later stage holds, and it holds pre-substitution source text -- so it belongs in no
// diagnostic and in no log.
type originalTexts map[ast.Node]string

// interpolator is what one stage-D run accumulates: the diagnostics every stage collects
// (stage.go), and the originals only this one records for W1.
type interpolator struct {
	stage
	env       EnvLookup
	originals originalTexts
}

// newInterpolator is one stage-D run over src, with its originals empty and its diagnostics
// tagged with the rule this stage raises.
func newInterpolator(src *source, env EnvLookup) *interpolator {
	return &interpolator{
		stage:     stage{src: src, rule: RuleInterpolate},
		env:       env,
		originals: originalTexts{},
	}
}

// interpolate runs stage D over the document rooted at root, substituting every reference in
// a value position and reporting every one it cannot.
//
// It rewrites root in place. The tree is the only thing it writes -- the source bytes behind
// src are never touched -- so a caller holding root after this call holds the substituted
// document, and one holding src still holds the file exactly as it was read.
//
// The policy is accumulate then stop, and the accumulation is the point: a configuration
// with four unresolved references is four diagnostics in one run, not four runs.
//
// Per occurrence means per occurrence *per message*, and the boundary matters to anyone
// counting. A diagnostic anchors on the value node, so two references written inside one
// scalar carry the same file, line and column; de-duplication (diag.go) keys on those
// together with the message, so two occurrences of the *same* variable in one scalar collapse
// to one diagnostic while two of *different* variables stay two. Reporting a column inside a
// scalar would need a column this package does not derive (ADR-4).
func interpolate(src *source, root ast.Node, env EnvLookup) (originalTexts, Errors) {
	pass := newInterpolator(src, env)

	// CONFORMANCE with skill Pattern 9 (interpolation) and skill Pattern 1 (walk
	// correctness). Both patterns prescribe this mechanism; neither is diverged from here.
	// Pattern 9 requires a structure-aware recursion that descends only into value positions
	// and explicitly rejects driving interpolation from a generic visitor, and Pattern 1
	// requires per-document traversal because *ast.File does not implement ast.Node (V1) --
	// which is already behind us, since root is the single document body stage C returned.
	//
	// A generic ast.Walk visitor is not merely avoided here, it cannot work: mapping keys,
	// anchor names and alias names are all *ast.StringNode, the identical type a scalar value
	// has (V2). A visitor that rewrote every *ast.StringNode handed to it would interpolate
	// inside keys and corrupt the identifiers anchor resolution depends on, and nothing about
	// the node itself distinguishes the four cases. Only the edge it was reached through
	// does, which is what substituteValuesUnder descends by.
	pass.substituteValuesUnder(root)

	return pass.originals, pass.diags
}

// substituteValuesUnder rewrites every value beneath node, and refuses every key that holds
// a reference.
//
// The node is one of three things: text to substitute into, a container, or a shape this
// stage does not know, which fails closed. Only a container has keys, and this recursion is
// the only thing that visits them at all -- so they are asked their own question on the way
// past, a different one from the question values are asked: a key is reported, never
// rewritten (AC #6).
//
// There is no guard against a nil node, deliberately. No parseable document produces one --
// an omitted value is an *ast.NullNode -- and were one to appear, the third case is where it
// lands and refusing it is the answer this stage wants. A guard returning early would be the
// silent walk-past the third case exists to prevent.
func (in *interpolator) substituteValuesUnder(node ast.Node) {
	if scalar, holdsText := scalarTextOf(node); holdsText {
		in.substitute(scalar)
		return
	}

	for _, key := range keyPositionsOf(node) {
		in.refuseReferenceInKey(key)
	}

	values, recognised := valuePositionsOf(node)
	if !recognised {
		// Fail closed. A shape this stage cannot descend is refused rather than walked past,
		// because walking past it is exactly how `${SECRET}` would survive into a loaded
		// configuration as literal text.
		//
		// The refusal is unconditional rather than conditional on the node's text holding a
		// reference, because a node's text is no witness of what it contains: the library's
		// sequence-entry wrapper renders as the empty string while holding the element it
		// wraps, so a text-reading guard would pass exactly the shapes it exists to catch.
		//
		// Nothing a document can hold today reaches here -- every value shape this library
		// version produces is enumerated in valuePositionsOf, which
		// TestEveryShapeADocumentCanHoldInAValuePositionIsRecognised asserts, so the branch is
		// unreachable now and loud on the upgrade that changes that.
		in.report(node, unrecognisedValueShape)
		return
	}
	for _, value := range values {
		in.substituteValuesUnder(value)
	}
}

// substitute applies the grammar to one scalar and records what became of it.
func (in *interpolator) substitute(scalar scalarText) {
	written := scalar.written()
	result := expand(written, in.env)

	for _, refused := range result.faults {
		in.report(scalar.anchor, refused)
	}
	if len(result.faults) != 0 {
		// A refused scalar keeps the text it was written with. The run stops at this stage so
		// nothing reads the value, and leaving it alone means the only thing any later reader
		// of this tree could find there is the reference its author actually wrote.
		return
	}

	if result.text != written {
		scalar.rewrite(result.text)
	}
	if result.fromEnvironment {
		in.originals[scalar.anchor] = written
	}
}

// refuseReferenceInKey is AC #6 at this stage. Interpolation applies to values only, so a key
// that reads like a reference is neither substituted nor left to mean something its author did
// not write: it is refused, at the key's own position.
//
// Which keys are refused, and for which of the two reasons, is keyreference.go's -- one decision
// asked here of every key an author wrote and asked again by stage E of every key the operations
// fold invents. This is the half that is stage D's: what to do about the answer.
func (in *interpolator) refuseReferenceInKey(key ast.Node) {
	if why, refused := keyReferenceFault(key); refused {
		in.report(key, why)
	}
}

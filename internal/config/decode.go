package config

import (
	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// This file is stage G: it turns the normalized document into the raw tree, and it cannot fail.
//
// GENUINE DIVERGENCE from skill Pattern 3 (unknown-key detection) and from the skill's Configuration
// note. Both leave `yaml.Strict()` available as a defence-in-depth check *alongside* the hand-rolled
// unknown-key walk; ADR-1 excludes every strictness option outright, and no DecodeOption is passed to
// NodeToValue anywhere in this package (TestNoStrictnessOptionIsPassedAnywhere,
// TestNodeToValueIsCalledWithNoDecodeOption).
//
// The reason is not tidiness, it is that the defence would be false assurance. V4 measured two things.
// Strict decode surfaces **one** unknown field per call and returns, so it cannot answer AC #8's ten
// keys at ten levels. And it is **blind inside a NodeUnmarshaler subtree**: measured on v1.19.2,
// `sub:\n  unknown_inner: 1` decoded with yaml.Strict() into a struct whose `sub` field is a
// NodeUnmarshaler returns no error at all. The two levels this package most needs unknown-key checking
// for -- `operations` and `headers` -- are exactly the levels reached through a NodeUnmarshaler here,
// so a reviewer who saw `Strict()` in this file would believe those levels were covered twice when
// they would not be covered at all. Defence in depth that is blind where the risk is concentrated is
// worse than none, because it stops anyone looking. `TestGoccyStrictDecodeIsBlindInsideAnUnmarshaler`
// is the measurement, so the reasoning can be reproduced rather than taken on trust.
//
// **What the guarantee inherits, and from where.** Every *field* of the raw tree is a wrapper that
// answers nil, so no field can abandon the document. The mapping the decoder is handed is a second
// question, and two of its failure modes are answered by an earlier stage rather than here:
//
//   - The root has to be a bare mapping node. The call below does not peel node properties the way
//     every other position in this package does, because stage C has already refused every root that is
//     not a mapping -- a tag and an anchor included. The tag is the half that makes this load-bearing:
//     measured on v1.19.2, NodeToValue refuses `!!map` at the root with `tag was used where mapping is
//     expected`, while it reads an anchored root straight through. So a stage C that began accepting a
//     tagged root would not fail loudly here; it would fill nothing at all and the document would be
//     reported clean (TestStageGIsOnlyEverHandedABareMappingRoot pins both the refusal and the
//     asymmetry).
//   - The mapping must not hold one key twice. `16:` and `0x10:` are the integer sixteen written
//     twice, the parser's own duplicate detection at stage B does not catch them, and NodeToValue then
//     fails with `duplicate key "16"` and no position at all. Stage F closes that gap and stops the
//     run, so such a document never arrives here (Step 5's Implementation Note 17;
//     TestTwoRenderingsOfOneKeyAreRefusedWithAPosition asserts the stop and
//     TestADuplicateTheParserMissesWouldOtherwiseFailTheDecoder the measurement that makes it
//     load-bearing).
//
// Both are invariants this stage relies on rather than hazards it defends against, which is why the
// error below is discarded rather than inspected: re-checking either here would put a second refusal
// of one authoring mistake after the stage that already worded it.
//
// The stage's two halves are here and in resolvewalk.go: the library fills the raw tree, and then
// every wrapper it filled meets the source -- which is when a position becomes derivable and a
// recorded refusal becomes a diagnostic.

// decodePass is what one stage-G run carries: what every stage accumulates (stage.go), tagged by
// default with the one rule this stage owns that the contract does not number.
//
// Every numbered rule it can raise is named by the wrapper that records it -- exactly one does, `Dur`
// with R40 -- so the field holds the structural refusal alone, as stage D's, E's and F's do.
type decodePass struct {
	stage
}

func newDecodePass(src *source) *decodePass {
	return &decodePass{stage: stage{src: src, rule: RuleDecode}}
}

// decodeDocument runs stage G over the document rooted at root.
//
// It **cannot fail**, and that is a structural property of the tree it decodes into rather than a
// promise about the documents it is given: every scalar of rawConfig is a wrapper that converts and
// returns nil (scalar.go), and every container is a wrapper that reads past the properties an author
// may write on it and returns nil (rawcontainer.go). There is therefore no field whose decode can
// answer non-nil, which is why NodeToValue's error is not part of this function's signature.
//
// The diagnostics it returns are exactly the conversion failures those wrappers recorded. The stage
// contributes none of its own: nothing here judges a value, which is stage H's subject, and nothing
// here judges a shape, which was stage F's (TestStageGReportsNothingButWrapperConversionFailures).
func decodeDocument(src *source, root ast.Node) (*rawConfig, Errors) {
	pass := newDecodePass(src)
	decoded := &rawConfig{}

	// No DecodeOption, per ADR-1 and the divergence recorded above. The error is discarded because
	// TestNoWrapperHasAnExitThatCanReturnAnError mechanically rejects a wrapper exit that returns
	// non-nil, while the two mapping-level failures are excluded by the stage-C root-property and
	// stage-F duplicate-key tests cited above.
	_ = yaml.NodeToValue(root, decoded)

	resolveEveryValue(pass, decoded)

	return decoded, pass.diags
}

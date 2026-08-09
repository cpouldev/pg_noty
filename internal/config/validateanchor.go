package config

// anchorClass is one of ADR-6's four places a diagnostic can point. The selector table is data so a
// rule chooses a class; it never derives a parser position of its own.
type anchorClass string

const (
	keyAnchor     anchorClass = "key token"
	mappingAnchor anchorClass = "enclosing mapping first key"
	valueAnchor   anchorClass = "value token"
	elementAnchor anchorClass = "element token"
)

type anchorSet struct {
	key, mapping, value, element Positioned
}

var semanticAnchors = map[anchorClass]func(anchorSet) Positioned{
	keyAnchor:     func(a anchorSet) Positioned { return a.key },
	mappingAnchor: func(a anchorSet) Positioned { return a.mapping },
	valueAnchor:   func(a anchorSet) Positioned { return a.value },
	elementAnchor: func(a anchorSet) Positioned { return a.element },
}

// semanticAnchorUse distinguishes clauses of one rule that point at different kinds of token. R19,
// R32 and R33 each need two uses; making the distinction data keeps that choice out of report sites.
type semanticAnchorUse string

const (
	scalarValueUse     semanticAnchorUse = "scalar value"
	structuredValueUse semanticAnchorUse = "structured value"
	headerNameUse      semanticAnchorUse = "header name"
	headerValueUse     semanticAnchorUse = "header value"
	listElementUse     semanticAnchorUse = "list element"
	effectiveValueUse  semanticAnchorUse = "effective comparison"
)

// stageHAnchorClass began as stage H's ADR-6 assignment table and remains the shared semantic
// selector. Step 11 adds effectiveValueUse so stage I reuses the same four anchor classes without
// conflating R39's range position with its effective comparison position.
var stageHAnchorClass = map[RuleID]map[semanticAnchorUse]anchorClass{
	R1:  {scalarValueUse: valueAnchor},
	R2:  {scalarValueUse: valueAnchor},
	R3:  {structuredValueUse: valueAnchor},
	R4:  {scalarValueUse: valueAnchor},
	R5:  {structuredValueUse: valueAnchor},
	R6:  {scalarValueUse: valueAnchor},
	R7:  {scalarValueUse: valueAnchor},
	R8:  {scalarValueUse: valueAnchor},
	R9:  {effectiveValueUse: valueAnchor},
	R10: {scalarValueUse: valueAnchor},
	R11: {effectiveValueUse: valueAnchor},
	R12: {effectiveValueUse: valueAnchor},
	R13: {scalarValueUse: valueAnchor},
	R14: {scalarValueUse: valueAnchor},
	R15: {scalarValueUse: valueAnchor},
	R16: {effectiveValueUse: valueAnchor},
	R17: {scalarValueUse: valueAnchor},
	R18: {scalarValueUse: valueAnchor},
	R19: {headerNameUse: keyAnchor, headerValueUse: valueAnchor},
	R21: {headerNameUse: keyAnchor},
	R23: {scalarValueUse: valueAnchor},
	R24: {structuredValueUse: valueAnchor},
	R25: {scalarValueUse: valueAnchor},
	R26: {structuredValueUse: valueAnchor},
	R29: {listElementUse: elementAnchor},
	R30: {structuredValueUse: valueAnchor},
	R31: {scalarValueUse: valueAnchor},
	R32: {structuredValueUse: valueAnchor, listElementUse: elementAnchor},
	R33: {structuredValueUse: valueAnchor, listElementUse: elementAnchor},
	R34: {scalarValueUse: valueAnchor},
	R35: {scalarValueUse: valueAnchor},
	R36: {structuredValueUse: valueAnchor},
	R37: {scalarValueUse: valueAnchor},
	R38: {listElementUse: elementAnchor},
	R39: {scalarValueUse: valueAnchor, effectiveValueUse: valueAnchor},
	R40: {scalarValueUse: valueAnchor},
	R43: {scalarValueUse: valueAnchor},
}

// reportSemantic is the scalar-rule entry point retained for stage G conversions and Step 8.
func reportSemantic(stage *stage, rule RuleID, anchors anchorSet, why fault) {
	reportSemanticUsing(stage, rule, scalarValueUse, anchors, why)
}

func reportSemanticUsing(
	stage *stage, rule RuleID, use semanticAnchorUse, anchors anchorSet, why fault,
) {
	class, assigned := stageHAnchorClass[rule][use]
	if !assigned {
		panic("semantic rule has no anchor class: " + rule + " / " + RuleID(use))
	}
	at := semanticAnchors[class](anchors)
	stage.reportPositionedAs(rule, at, why)
}

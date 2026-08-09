package config

import "testing"

// This file closes the containment grid's schema-position dimension over schema.go's own table, in
// both directions: every position the table declares is planted at, and every sensitivity it
// declares is reached. It is separate from containmentschemapositions_test.go, which builds the
// dimension, only because that file reached its mechanical line budget.

// TestTheSchemaPositionDimensionCoversEveryDeclaredPosition closes the family over the table.
//
// The count is asserted against declaredSchemaPositions rather than against a number written beside
// it, so adding a key to schema.go fails here until the grid plants at it -- which is the mechanism
// a prose note asking a later author to remember was tried in place of, and did not hold.
func TestTheSchemaPositionDimensionCoversEveryDeclaredPosition(t *testing.T) {
	layouts := schemaPositionLayouts()

	planted := make(map[string]bool, len(layouts))
	for _, layout := range layouts {
		planted[layout.name] = true
	}

	const theControl = 1
	plantable := theControl
	for _, at := range declaredSchemaPositions() {
		if !plantableInTheGrid(at) {
			continue
		}
		plantable++
		if !planted["a secret written at "+at.locator] {
			t.Errorf("no layout plants at %s, a position whose contract hides a container holding "+
				"a sensitive leaf", at.locator)
		}
	}
	if len(layouts) != plantable {
		t.Fatalf("%d schema-position layouts for the %d positions the grid can plant at; a "+
			"declared position with no layout is an arm no containment run reaches",
			len(layouts), plantable)
	}
}

// TestTheSchemaPositionDimensionReachesEverySensitivityTheTableDeclares is the other half: covering
// every *position* is worth nothing if the positions between them exercise one arm.
//
// Each sensitivity is asserted separately rather than as a set, because a dimension reaching two of
// the three would satisfy any "some position is sensitive" check while leaving the third's arm
// unreached.
func TestTheSchemaPositionDimensionReachesEverySensitivityTheTableDeclares(t *testing.T) {
	reached := map[sensitivity]int{}
	for _, at := range declaredSchemaPositions() {
		reached[at.sensitive]++
	}

	for _, declared := range []struct {
		kind sensitivity
		name string
	}{
		{kind: publicValue, name: "publicValue"},
		{kind: urlPassword, name: "urlPassword"},
		{kind: entireValue, name: "entireValue"},
	} {
		if reached[declared.kind] == 0 {
			t.Errorf("no declared position holds %s, so the walk's arm for it is unreachable from "+
				"every layout this family generates", declared.name)
		}
	}
}

package config

import "testing"

// This file is the containment grid's schema-position dimension.
//
// Every layout written out by hand plants at one of four positions -- `signing.secrets`,
// `database.url`, `database.listen_url`, and a sensitive leaf beneath a misspelled parent. The
// property those layouts are quantified over reads as "no secret in any layout, in any key
// spelling", which is universal in two dimensions and silent in the third: the schema walk branches
// on what the *table* says at a position, and four positions reach three of its arms. Four million
// executions therefore said nothing about the arm that answered "nothing here is sensitive" for
// every declared public key holding a container.
//
// The dimension is generated from schema.go's own table rather than listed, so a key added there
// joins the grid by being declared, and the count is pinned so a table that grew without the family
// growing with it fails here by name.

// theUndeclaredControlPosition is a name no level declares, planted at the root beside the declared
// ones. It is the control: the walk's undeclared-key arm has always failed closed, so a run in which
// *every* position hides its secret proves nothing unless one of them is known to travel a different
// arm. It is spelled as a near-miss of a declared key so that the suggestion machinery sees the same
// shape a user's typo would.
const theUndeclaredControlPosition = "databse"

// schemaPositionLayouts plants a container holding a sensitive leaf at every position whose
// contract hides one, plus the undeclared control.
//
// A *container* rather than a bare secret, because a bare secret written at a declared public scalar
// is not a secret at all -- `worker.concurrency` holding a connection string is a wrong value the
// diagnostic exists to quote, and TestADeclaredPublicScalarKeepsAConnectionStringWrittenAsItsValue
// requires it kept. What the walk got wrong was the shape: a public *name* holding a container is a
// context the table does not describe, and every arm that answered it from the name rendered what
// was inside. So the plant is the shape whose answer the contract makes uniform.
//
// The positions the contract does *not* hide it at are left out rather than planted and excused,
// because the grid's claim is unconditional and a layout it may render is a layout that has to be
// special-cased in every subject that crosses it. Which positions those are, and why, is pinned by
// TestTheSchemaPositionDimensionLeavesOutOnlyItsThreeKnownReasons.
func schemaPositionLayouts() []secretLayout {
	var layouts []secretLayout

	for _, at := range declaredSchemaPositions() {
		if !plantableInTheGrid(at) {
			continue
		}
		layouts = append(layouts, layoutPlantingAt(at, "a secret written at "+at.locator))
	}
	return append(layouts, layoutPlantingAt(
		schemaPosition{steps: []schemaStep{{name: theUndeclaredControlPosition}}},
		"a secret written beneath the undeclared control key"))
}

// layoutPlantingAt is one position's layout. The position is captured by value, so each closure
// writes its own path rather than the last one the loop visited.
func layoutPlantingAt(at schemaPosition, name string) secretLayout {
	return secretLayout{
		name: name,
		body: func(secret string, key keySpelling) string {
			held := "{" + theSensitiveProbeKey + ": " +
				yamlQuoted(connectionStringHiding(secret, "db.internal:5432")) + "}"

			return at.documentWriting(held, key)
		},
	}
}

// theGridsOwnRootKey is the key plantedOnBothBranches writes to introduce every document, and writes
// a second time to make the fallback variant unparseable.
//
// A layout planting at it would write that key twice in the row declared *path-aware*, so that row
// would not parse and would assert about a branch it never reached rather than about the position.
// $.version is therefore reachable by the deterministic position subject and not by this grid, and
// it is covered there: TestEveryDeclaredKeyHoldingAContainerFailsClosed writes its documents with no
// preamble.
const theGridsOwnRootKey = "version"

// plantableInTheGrid reports whether the grid can write a document planting at this position that
// still reaches the branch its row declares.
func plantableInTheGrid(at schemaPosition) bool {
	return hidesAContainerHoldingASensitiveLeaf(at) && at.steps[0].name != theGridsOwnRootKey
}

// namesTheProbeKeyAsPublic reports whether a level declares the probe key and declares it public.
// That is destination.url and nothing else: schema.go writes it with loggedHolding, so its source
// stays visible in a diagnostic while its resolved credentials never enter a log.
func namesTheProbeKeyAsPublic(level mappingLevel) bool {
	spec, named := level.key(theSensitiveProbeKey)
	return named && spec.sensitive == publicValue
}

// TestTheSchemaPositionDimensionLeavesOutOnlyItsThreeKnownReasons names every position the family
// declines to plant at, so a position dropped for any fourth reason fails here rather than quietly
// narrowing the dimension.
//
// Two reasons are the contract's own, and both are positions where a container holding a sensitive
// leaf is *rendered* by design -- a grid row that must render is one the grid has no way to say.
// A free-form level makes every name public, because `headers: {url: x}` writes a header named url
// and a header name is not a configuration key; that divergence is the point of having two branches
// and is pinned by TestAFreeFormHeaderNamedLikeASensitiveKeyRemainsPublic. A declared level that
// names the probe key and declares it public is destination, whose url the contract shows in
// diagnostics on purpose. The third reason is the harness's own root key.
//
// All three are also required to be *used*, because a reason no position needs is a clause that
// cannot fail.
func TestTheSchemaPositionDimensionLeavesOutOnlyItsThreeKnownReasons(t *testing.T) {
	freeForm, publicLeaf, harnessKey := 0, 0, 0

	for _, at := range declaredSchemaPositions() {
		if plantableInTheGrid(at) {
			continue
		}
		switch level, declared := schemaLevels[at.child]; {
		case declared && level.freeForm:
			freeForm++
		case declared && namesTheProbeKeyAsPublic(level):
			publicLeaf++
		case at.steps[0].name == theGridsOwnRootKey:
			harnessKey++
		default:
			t.Errorf("the family plants nothing at %s, which is none of the free-form divergence, "+
				"a level declaring %q public, or the harness's own root key; a position left out "+
				"for any other reason is an arm no containment run reaches",
				at.locator, theSensitiveProbeKey)
		}
	}

	for _, clause := range []struct {
		used int
		why  string
	}{
		{used: freeForm, why: "the free-form divergence"},
		{used: publicLeaf, why: "a level declaring " + theSensitiveProbeKey + " public"},
		{used: harnessKey, why: "the harness's own root key " + theGridsOwnRootKey},
	} {
		if clause.used == 0 {
			t.Errorf("no position is left out for %s, so that clause cannot fail", clause.why)
		}
	}
}

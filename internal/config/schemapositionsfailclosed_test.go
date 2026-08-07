package config

import (
	"strings"
	"testing"
)

// This file runs D3's fail-closed claim at every position schema.go declares, in both directions.
//
// Three arms of the schema walk used to answer "nothing here is sensitive" by returning nothing: a
// free-form level, which declares every *name* public and no *shape*; a declared key naming no
// child level, which describes a scalar leaf and says nothing about a container written beneath it;
// and a declared level reached at a node that is not a mapping. All three are decisions about names
// silently answering a question about shapes, and each rendered a connection string verbatim on the
// branch that runs for every document that parses.
//
// The expectation is derived from the table per position rather than listed, so a key added to
// schema.go is judged by the contract it is declared with rather than by a row somebody remembered
// to add.

// theSensitiveProbeKey is the leaf name planted inside the container. It is a contract name, so it
// is written as the literal the contract states and its sensitivity is asserted rather than assumed.
const theSensitiveProbeKey = "url"

// TestEveryDeclaredKeyHoldingAContainerFailsClosed plants a mapping holding a sensitive leaf at
// every declared position and requires the secret to be gone wherever the table does not declare
// that leaf public.
func TestEveryDeclaredKeyHoldingAContainerFailsClosed(t *testing.T) {
	if !namesASensitiveKey(theSensitiveProbeKey) {
		t.Fatalf("%q is not a name the table declares sensitive, so every row below is vacuous",
			theSensitiveProbeKey)
	}

	positions := declaredSchemaPositions()
	if len(positions) == 0 {
		t.Fatal("no declared positions, so this test would pass vacuously")
	}
	for _, at := range positions {
		t.Run(at.locator, func(t *testing.T) {
			container := "{" + theSensitiveProbeKey + ": " +
				yamlQuoted(connectionStringHiding(leakSentinel+"BENEATH", "db.internal")) + "}"

			assertPlantedAtPositionIsHidden(t, at, container, hidesAContainerHoldingASensitiveLeaf(at))
		})
	}
}

// TestADeclaredPublicScalarKeepsAConnectionStringWrittenAsItsValue is the bound on the same claim. A
// name the table declares, and declares public, keeps what is written as its scalar value --
// otherwise the fail-closed answer above would have grown into "hide everything", which costs a
// diagnostic the context it exists to show.
func TestADeclaredPublicScalarKeepsAConnectionStringWrittenAsItsValue(t *testing.T) {
	for _, at := range declaredSchemaPositions() {
		t.Run(at.locator, func(t *testing.T) {
			scalar := yamlQuoted(connectionStringHiding(leakSentinel+"SCALAR", "db.internal"))

			assertPlantedAtPositionIsHidden(t, at, scalar, at.sensitive != publicValue)
		})
	}
}

// hidesAContainerHoldingASensitiveLeaf is what the contract says about a mapping holding a
// sensitive leaf name written at this position.
//
// A declaration that hides the value whole hides whatever shape was written as it. Otherwise the
// question moves one level down, to the level this key names.
//
// A level that names no key describes a scalar leaf and describes nothing about a container, so a
// container beneath it is an undescribed context and fails closed. A declared level either names
// the leaf or does not; only a level that names it *and* declares it public keeps it, which is
// destination.url and nothing else.
//
// **A free-form level is the deliberate exception, and it is a divergence between D3's two
// branches.** `headers: {url: x}` writes a header *named* url, and a header name is not a
// configuration key: the level declares every name public, so its value renders on the path-aware
// branch while the key-scoped fallback -- which has no path to tell a header name from a database
// key -- blanks it. That divergence is the point of having two branches and is pinned by
// TestAFreeFormHeaderNamedLikeASensitiveKeyRemainsPublic. It stops at the names: a *container*
// written as a header value is a shape the level does not describe, which is the
// $.defaults.headers.X-Probe row below.
func hidesAContainerHoldingASensitiveLeaf(at schemaPosition) bool {
	if at.sensitive != publicValue {
		return true
	}

	level, declared := schemaLevels[at.child]
	if !declared {
		return true
	}
	if level.freeForm {
		return false
	}
	spec, named := level.key(theSensitiveProbeKey)
	return !named || spec.sensitive != publicValue
}

// assertPlantedAtPositionIsHidden renders a diagnostic on every line of a document that writes
// value at one position, and asserts both directions of the contract's answer.
func assertPlantedAtPositionIsHidden(t *testing.T, at schemaPosition, value string, hidden bool) {
	t.Helper()

	document := at.documentWriting(value, plainKey)
	if _, pathAware := branchTaken(plantedSecret{text: document}); !pathAware {
		t.Fatalf("the document does not parse, so it reads on the fallback rather than on the "+
			"path-aware branch this case is written for:\n%s", document)
	}

	rendered := renderEveryLineOf(document)
	if strings.Contains(rendered, leakSentinel) == hidden {
		t.Errorf("the secret written at %s is %s; the table declares sensitivity %d and the shape "+
			"%q beneath it\nsource:\n%s\nrendered:\n%s",
			at.locator, renderedOrHidden(hidden), at.sensitive, at.child, document, rendered)
	}
}

func renderedOrHidden(hidden bool) string {
	if hidden {
		return "rendered, and the contract hides it"
	}
	return "hidden, and the contract renders it"
}

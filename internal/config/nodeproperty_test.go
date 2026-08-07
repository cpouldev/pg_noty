package config

import "testing"

// The node properties a document may write on the values and keys stage F judges, closed over the arms of
// the reader it goes through.
//
// The gap this file closes was real and was found by mutation: `checkValue` reads past a value's
// properties before asserting its shape, and nothing generated a value carrying any. Removing the
// peel left the whole suite green, while `database: !!map {url: x}` -- legal YAML -- would have been
// refused as a shape the contract has no word for. A tag is not stripped by stage E, because a tag
// is a claim *about* a value rather than a value of its own, so stage F is the first reader that has
// to look past one.
//
// Anchors are a different case and both belong here. Stage E removes an anchor from a *value*
// position, so the rows that write one assert the pipeline as a whole rather than this stage's
// reader; an anchor on a *key* survives into this stage, and is read past by keyTextOf.

// propertySpelling is one complete, valid configuration in which one declared key's value or key
// carries YAML node properties. Every row must load with nothing reported: each is a legal document
// that differs from the plain spelling only in a property.
var propertySpellings = map[string]string{
	"a plain mapping": "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\nlisteners: []\n",
	"a tagged mapping": "version: 1\ndatabase: !!map {url: postgres://noty:pw@db/noty}\n" +
		"listeners: []\n",
	"an anchored mapping": "version: 1\ndatabase: &db {url: postgres://noty:pw@db/noty}\n" +
		"listeners: []\n",
	"an anchored tagged mapping": "version: 1\ndatabase: &db !!map {url: postgres://noty:pw@db/noty}\n" +
		"listeners: []\n",

	"a tagged scalar":    "version: !!int 1\ndatabase:\n  url: postgres://noty:pw@db/noty\nlisteners: []\n",
	"an anchored scalar": "version: &v 1\ndatabase:\n  url: postgres://noty:pw@db/noty\nlisteners: []\n",
	"an alias to a scalar": "version: 1\ndatabase:\n  url: &u postgres://noty:pw@db/noty\n" +
		"  listen_url: *u\nlisteners: []\n",

	"a tagged sequence":    "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\nlisteners: !!seq []\n",
	"an anchored sequence": "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\nlisteners: &l []\n",

	// A list *element*, which is the arm the walk's own peel exists for: `checkValue` reads the
	// properties off the list, and each element may then carry its own.
	"a tagged list element": "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\n" +
		"listeners:\n  - !!map\n    name: order_paid\n    table: public.orders\n" +
		"    operations: [insert]\n    destination:\n      url: https://h.test/x\n",
	"an anchored list element": "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\n" +
		"listeners:\n  - &first\n    name: order_paid\n    table: public.orders\n" +
		"    operations: [insert]\n    destination:\n      url: https://h.test/x\n",

	// The key side. Both wrappers carry the key one level down while the node's own token is only
	// the introducer, so a stage reading that token would find `!!str` or `&k` and report the
	// declared key as unknown.
	"a tagged key":    "version: 1\n!!str database:\n  url: postgres://noty:pw@db/noty\nlisteners: []\n",
	"an anchored key": "version: 1\n&k database:\n  url: postgres://noty:pw@db/noty\nlisteners: []\n",
	"an explicit key": "version: 1\n? database\n:\n  url: postgres://noty:pw@db/noty\nlisteners: []\n",
}

// TestEveryNodePropertyADeclaredValueAdmitsIsJudgedByTheShapeBeneathIt is the claim: a property is
// not part of the shape, so a document that writes one is the same document to this stage.
func TestEveryNodePropertyADeclaredValueAdmitsIsJudgedByTheShapeBeneathIt(t *testing.T) {
	for spelling, document := range propertySpellings {
		t.Run(spelling, func(t *testing.T) {
			diags, decodable := stageF(t, document)

			if len(diags) != 0 {
				t.Errorf("the structural pass reported %q on a legal document", messagesOf(diags))
			}
			if !decodable {
				t.Error("a property on a value left the document undecodable")
			}
		})
	}
}

// TestThePropertySpellingsReachTheStageTheyAreWrittenFor keeps the rows above from asserting
// nothing. Stage E strips an anchor from a value position and folds nothing here, so a row whose
// property no longer arrives at stage F would pass by having had it removed rather than by having
// it read past. This asserts the three tagged rows still carry their tag when the pass runs.
func TestThePropertySpellingsReachTheStageTheyAreWrittenFor(t *testing.T) {
	tagged := map[string]string{
		"a tagged mapping":      "$.database",
		"a tagged scalar":       "$.version",
		"a tagged sequence":     "$.listeners",
		"a tagged list element": "$.listeners[0]",
	}

	for spelling, path := range tagged {
		t.Run(spelling, func(t *testing.T) {
			_, root, faults := stageE(t, propertySpellings[spelling], corpusVariables)
			if len(faults) != 0 {
				t.Fatalf("the fixture does not reach stage F: %q", messagesOf(faults))
			}

			held := nodeIn(t, root, path)
			if _, named := nodeKindOf(held); named {
				t.Errorf("%s arrives at stage F as %T, which the contract already has a word for; "+
					"the row no longer exercises reading past a property", spelling, held)
			}
		})
	}
}

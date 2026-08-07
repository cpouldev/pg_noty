package config

import (
	"strings"
	"testing"
)

// Why the pipeline runs its stages in the order it does, asserted rather than left to a fixture
// that happens to notice. Three orderings are load-bearing so far: stage D before stage E, stage
// E before stage F, and stage F before stage G.
//
// load.go states each requirement at its call site and the corpus fixtures would fail if either
// were broken, but they would fail as "a valid document renders something" -- a symptom that names
// no stage at all. The cases here name the ordering, so a walk that changed underneath one is read
// here first. load_test.go asserts the complementary claim: that the machine calls its stages in
// this order at all, so a reordering these documents happened not to expose still fails.

// anchoredEscape is the shape the ordering is about: one escaped delimiter written once, inside
// an anchor two aliases reach. One position, three readers.
const anchoredEscape = "base: &base\n  header: $${NOT_A_REFERENCE}\nfirst: *base\nsecond: *base\n"

// TestStageDBeforeStageEUnescapesAnAnchoredEscapeExactlyOnce is the wired order. Substitution
// visits the anchored scalar once, because an alias site holds a name rather than content, so
// the escape is unescaped once however many aliases read the result.
func TestStageDBeforeStageEUnescapesAnAnchoredEscapeExactlyOnce(t *testing.T) {
	root := normalized(t, anchoredEscape)

	for _, path := range []string{"$.base.header", "$.first.header", "$.second.header"} {
		if got := textIn(t, root, path); got != "${NOT_A_REFERENCE}" {
			t.Errorf("%s = %q, want the escape unescaped exactly once", path, got)
		}
	}
}

// TestStageEBeforeStageDTurnsAnAnchoredEscapeIntoAnUndefinedReference runs the two stages in the
// forbidden order over that same document, and is what makes the ordering a guard rather than an
// accident.
//
// Stage E replaces every alias with the anchored node itself, so afterwards one scalar occupies
// three value positions. Stage D's walk carries no set of visited nodes -- and needs none while
// each anchored scalar is a single position -- so it would substitute into that scalar once per
// position: the first visit turns `$${NOT_A_REFERENCE}` into the text `${NOT_A_REFERENCE}`, and
// the next two read that text as a reference to a variable nobody set.
//
// A change to either walk that made this order harmless would fail here, which is the point: the
// requirement is then no longer load-bearing and load.go's call-site rationale needs re-deriving.
func TestStageEBeforeStageDTurnsAnAnchoredEscapeIntoAnUndefinedReference(t *testing.T) {
	src := newSource(interpolationFixture, []byte(anchoredEscape))
	root, parsed := parseDocument(src)
	if len(parsed) != 0 {
		t.Fatalf("the fixture does not parse: %q", messagesOf(parsed))
	}

	if diags := normalize(src, root); len(diags) != 0 {
		t.Fatalf("normalizing reported %q", messagesOf(diags))
	}
	_, diags := interpolate(src, root, MapEnv(nil))

	if len(diags) == 0 {
		t.Fatal("stage E before stage D reported nothing; the ordering load.go requires no longer " +
			"protects the escape, so re-derive why stage D must run first")
	}
	for _, diag := range diags {
		if diag.Rule != RuleInterpolate || !strings.Contains(diag.Msg, "NOT_A_REFERENCE") {
			t.Errorf("the wrong order reported %q from %q, want the unescaped text read as a reference",
				diag.Msg, diag.Rule)
		}
	}
}

// mergedAndFolded is the shape the E-before-F ordering is about, and it holds both of the
// constructs stage E removes: a listener whose retry block is inherited through YAML's own merge
// key, and an operations set written in the list form.
//
// Lines 5 and 6 declare the anchor; line 13 merges it.
const mergedAndFolded = `version: 1
database:
  url: postgres://noty:pw@db.internal:5432/noty
defaults:
  retry: &base
    max_attempts: 5
listeners:
  - name: order_paid
    table: public.orders
    operations: [insert]
    destination:
      url: https://hooks.example.test/order-paid
    retry:
      <<: *base
`

// TestStageEBeforeStageFMeetsTheOneShapeTheContractDeclares is the wired order. Stage F is
// promised exactly one spelling per construct, and both of this document's are reduced to it
// before the structural pass ever sees them.
func TestStageEBeforeStageFMeetsTheOneShapeTheContractDeclares(t *testing.T) {
	if diags, decodable := stageF(t, mergedAndFolded); len(diags) != 0 || !decodable {
		t.Errorf("the structural pass reported %q on a document whose every construct stage E reduced",
			messagesOf(diags))
	}
}

// TestStageFBeforeStageEReportsTheConstructsStageEWasGoingToRemove runs the two stages in the
// forbidden order over that same document, and is what makes the ordering a guard rather than an
// accident.
//
// Both constructs become diagnostics about the author's own legal YAML. The merge marker is a key
// the contract declares at no level, so it is reported as unknown -- and the keys it was going to
// bring in are reported as absent alongside it, if they were required. The operations list is
// still a list, so every name in it is judged against the mapping the contract declares there.
//
// A change to either stage that made this order harmless would fail here, which is the point: the
// requirement would then no longer be load-bearing and load.go's call-site rationale would need
// re-deriving.
func TestStageFBeforeStageEReportsTheConstructsStageEWasGoingToRemove(t *testing.T) {
	src := newSource(interpolationFixture, []byte(mergedAndFolded))
	root, parsed := parseDocument(src)
	if len(parsed) != 0 {
		t.Fatalf("the fixture does not parse: %q", messagesOf(parsed))
	}
	if _, diags := interpolate(src, root, MapEnv(nil)); len(diags) != 0 {
		t.Fatalf("interpolating reported %q", messagesOf(diags))
	}

	diags, _ := checkShape(src, root)

	if len(diags) == 0 {
		t.Fatal("stage F before stage E reported nothing; the ordering load.go requires no longer " +
			"protects the two constructs, so re-derive why stage E must run first")
	}
	for _, diag := range diags {
		if diag.Rule != R41 && diag.Rule != RuleShape {
			t.Errorf("the wrong order reported %q from %q, want the unreduced constructs judged as shape",
				diag.Msg, diag.Rule)
		}
	}
}

// misshapenBeside is the shape the F-before-G ordering is about: a value whose YAML shape the contract
// does not admit, beside a value of the right shape whose text no wrapper can read.
//
// `database` is declared a mapping and holds a scalar, which is the class stage F stops on. `concurrency`
// is a scalar holding text where the contract declares an integer, which is the class stage G records.
const misshapenBeside = "version: 1\nworker:\n  concurrency: abc\ndatabase: text\nlisteners: []\n"

// TestStageFBeforeStageGReportsTheShapeAndNotTheValue is the wired order. One authoring mistake is one
// diagnostic, and a document whose shape the contract rejects has no readable value to complain about --
// so the run ends with stage F's finding and stage G never sees the document.
func TestStageFBeforeStageGReportsTheShapeAndNotTheValue(t *testing.T) {
	_, _, errs := Parse([]byte(misshapenBeside), interpolationFixture, MapEnv(nil))

	if len(errs) != 1 || errs[0].Rule != RuleShape {
		t.Errorf("Parse reported %q, want only the shape stage F stops on", messagesOf(errs))
	}
}

// TestStageGBeforeStageFReportsTheValueTwiceOver runs the two stages in the forbidden order over that
// same document, and is what makes the ordering a guard rather than an accident.
//
// Decode does not abandon a document at its first failure -- measured, and pinned by
// TestGoccyContinuesDecodingSiblingsAfterAFailure -- so running it first records the conversion failure
// as well, and an author is told two things about a file with one mistake they can act on. The shape has
// to be fixed before the value beneath it means anything.
//
// A change to either stage that made this order harmless would fail here, which is the point: the
// requirement would then no longer be load-bearing and load.go's call-site rationale would need
// re-deriving.
func TestStageGBeforeStageFReportsTheValueTwiceOver(t *testing.T) {
	src := newSource(interpolationFixture, []byte(misshapenBeside))
	root, parsed := parseDocument(src)
	if len(parsed) != 0 {
		t.Fatalf("the fixture does not parse: %q", messagesOf(parsed))
	}
	if _, diags := interpolate(src, root, MapEnv(nil)); len(diags) != 0 {
		t.Fatalf("interpolating reported %q", messagesOf(diags))
	}
	if diags := normalize(src, root); len(diags) != 0 {
		t.Fatalf("normalizing reported %q", messagesOf(diags))
	}

	_, conversions := decodeDocument(src, root)
	shape, decodable := checkShape(src, root)

	if decodable {
		t.Fatal("stage F no longer stops on this document, so the ordering it protects is a different one")
	}
	if len(conversions) == 0 {
		t.Fatal("stage G before stage F reported nothing; the ordering load.go requires no longer keeps " +
			"one mistake to one diagnostic, so re-derive why stage F must run first")
	}
	if total := len(shape) + len(conversions); total <= len(shape) {
		t.Errorf("the wrong order reported %d diagnostics for %d shape mistakes", total, len(shape))
	}
}

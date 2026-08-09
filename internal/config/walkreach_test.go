package config

import (
	"slices"
	"strings"
	"testing"
)

// Which mappings the walk reaches, asked of the table the walk is driven by rather than of a
// document somebody wrote out.
//
// The claim is that no level can be silently unchecked, and a hand-written document cannot make it:
// a level added to schemaLevels would simply not appear in it, so the test would keep passing while
// the property it names quietly narrowed. Both the document and the expected locators are therefore
// generated from schemaLevels, and the levels the generation visited are counted against it, so a
// level the table declares and no key reaches fails here by name.

// strayKey is the name planted in every mapping the contract declares. No level declares it, and it
// is far past the suggestion threshold from every declared name, so each one it produces is exactly
// one unknown-key diagnostic carrying that mapping's own locator.
const strayKey = "notdeclaredanywhere"

// reachProbe is one generated document: the lines it is written out of, the locator every stray
// name in it should be reported at, and the levels the generation walked into.
type reachProbe struct {
	t       *testing.T
	lines   []string
	want    []string
	visited map[levelName]bool
}

// plant writes the stray name into the mapping this level describes, and then everything needed to
// reach the mappings beneath it.
//
// A free-form level contributes a stray and no expectation, which is SC-9 asserted rather than
// assumed: a header mapping has no vocabulary, so the name is legal there and a diagnostic about it
// would be the exemption failing.
func (p *reachProbe) plant(name levelName, level mappingLevel, indent, locator string, ancestors []levelName) {
	p.visited[name] = true
	p.write(indent, strayKey+": 1")

	if !level.freeForm {
		p.want = append(p.want, locator+strayKey)
	}

	for _, spec := range level.keys {
		p.plantKey(spec, indent, locator, ancestors)
	}
}

// plantKey writes what one declared key contributes to that document: the shape beneath it, so the
// walk has somewhere to descend, or a value, so a key the contract requires is not also reported
// missing and counted here. A key that is neither is left out, because the document states only
// what the reach claim needs.
func (p *reachProbe) plantKey(spec keySpec, indent, locator string, ancestors []levelName) {
	child, declaresShape := schemaLevels[spec.child]
	if !declaresShape {
		if spec.required() {
			// Stage F judges the shape of a value and never the value, so any scalar serves.
			p.write(indent, spec.name+": 1")
		}
		return
	}
	if slices.Contains(ancestors, spec.child) {
		// Without this the generation would not terminate, and a hang says far less than a named
		// failure about a table that has become cyclic.
		p.t.Fatalf("level %q is reachable from itself through %q", spec.child, locator+spec.name)
	}

	p.write(indent, spec.name+":")
	beneath := append(slices.Clone(ancestors), spec.child)

	// A key whose value is only ever a sequence reaches its shape through an element, which is the
	// same reading of the table that schema_test.go's path walk takes.
	if spec.kinds == sequenceValue {
		p.plantElement(spec.child, child, indent, locator+spec.name+"[0].", beneath)
		return
	}
	p.plant(spec.child, child, indent+"  ", locator+spec.name+".", beneath)
}

// plantElement is the same for a shape reached through a list. The `- ` opening the item replaces
// the element's indent rather than being written before it, so both are four characters wide and
// the item's remaining keys line up beneath its first.
func (p *reachProbe) plantElement(name levelName, level mappingLevel, indent, locator string, ancestors []levelName) {
	const elementIndent = "    "
	opens := len(p.lines)

	p.plant(name, level, indent+elementIndent, locator, ancestors)
	p.lines[opens] = indent + "  - " + strings.TrimPrefix(p.lines[opens], indent+elementIndent)
}

func (p *reachProbe) write(indent, text string) { p.lines = append(p.lines, indent+text) }

// document is the generated configuration.
func (p *reachProbe) document() string { return strings.Join(p.lines, "\n") + "\n" }

// TestTheWalkReachesEveryLevelTheContractDeclares is the completeness claim behind AC #8. The ten
// levels the criterion enumerates are the ones it names; this asserts the walk reaches every
// mapping the table declares, at every path it declares one, so a level the criterion does not
// mention cannot be silently unchecked.
//
// A level the walk never descends into shows up as a missing locator naming that level, rather than
// as a count that is one short.
func TestTheWalkReachesEveryLevelTheContractDeclares(t *testing.T) {
	probe := &reachProbe{t: t, visited: make(map[levelName]bool)}
	probe.plant(levelRoot, schemaLevels[levelRoot], "", "", []levelName{levelRoot})

	for name := range schemaLevels {
		if !probe.visited[name] {
			t.Errorf("level %q is declared and no key reaches it, so no walk can ever check it", name)
		}
	}
	if len(probe.want) == 0 {
		t.Fatal("the generated document plants no stray name, so this would assert nothing")
	}
	slices.Sort(probe.want)

	diags, decodable := stageF(t, probe.document())

	if got := pathsOf(diags); !slices.Equal(got, probe.want) {
		t.Errorf("the walk reached\n got %q\nwant %q\nfrom:\n%s", got, probe.want, probe.document())
	}
	if !decodable {
		t.Errorf("an unknown key stopped the run; only a shape the later stages cannot read may")
	}
}

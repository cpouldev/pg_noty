package config

import (
	"strings"
	"testing"
)

// This file is the structural regression net for the key-spelling question. It runs every spelling
// YAML permits for a leaf key, in every value shape, on both of D3's branches, and asserts the same
// containment claim in each cell -- so a spelling one branch reads and the other does not fails here
// by name.

// TestBothBranchesContainASecretWrittenInEverySpellingTheGrammarPermits is the grid: twelve
// spellings, three value shapes, two branches. It is quantified over the grammar rather than over
// sensitiveKeyForms, which is the whole point -- a cell can only be green because the redactor
// governs that spelling, never because the generator declined to write it.
func TestBothBranchesContainASecretWrittenInEverySpellingTheGrammarPermits(t *testing.T) {
	secret := secretMarkedOnEveryLine("plain")

	for _, spelling := range keySpellingsTheGrammarPermits {
		for _, shape := range valueShapesTheGrammarPermits {
			for _, planted := range plantsSpelling(secret, spelling, shape) {
				t.Run(planted.where+", "+planted.branch, func(t *testing.T) {
					assertNothingLeaks(t, planted)
				})
			}
		}
	}
}

// TestTheGrammarGridRunsACellPerSpellingShapeAndBranch pins the grid's size against its three
// dimensions, so a dimension that stopped being crossed -- or a spelling whose two branch variants
// collapsed into one -- fails here rather than shrinking the property in silence.
func TestTheGrammarGridRunsACellPerSpellingShapeAndBranch(t *testing.T) {
	const branches = 2

	cells := 0
	for _, spelling := range keySpellingsTheGrammarPermits {
		for _, shape := range valueShapesTheGrammarPermits {
			cells += len(plantsSpelling(secretMarkedOnEveryLine("plain"), spelling, shape))
		}
	}

	want := len(keySpellingsTheGrammarPermits) * len(valueShapesTheGrammarPermits) * branches
	if cells != want {
		t.Fatalf("the grammar grid runs %d cells, want the %d its %d spellings x %d shapes x %d "+
			"branches declare", cells, want,
			len(keySpellingsTheGrammarPermits), len(valueShapesTheGrammarPermits), branches)
	}
	if want != 72 {
		t.Errorf("the grammar grid is %d cells; it is the 12 x 3 x 2 net the key-spelling drift was "+
			"found with, so a smaller one has to be argued for rather than arrived at", want)
	}
}

// TestEveryGrammarSpellingIsWrittenDifferently keeps twelve rows from being fewer spellings than
// twelve: two rows writing the same bytes would report full coverage of a dimension one of them
// never varies.
func TestEveryGrammarSpellingIsWrittenDifferently(t *testing.T) {
	written := make(map[string]string, len(keySpellingsTheGrammarPermits))

	for _, spelling := range keySpellingsTheGrammarPermits {
		declaration := spelling.write(sensitiveKeyOfTheGrammarGrid, grammarProbeValue)
		if first, seen := written[declaration]; seen {
			t.Errorf("the %s and %s rows both write %q", first, spelling.name, declaration)
		}
		written[declaration] = spelling.name
		if spelling.clause == "" {
			t.Errorf("the %s row cites no clause, so nothing says which grammar admits it", spelling.name)
		}
	}
}

// TestTheGrammarGridWritesBothKeyColonPlacements keeps the list from drifting back to the class the
// matcher already handles. The split placement -- the colon on a line the name is not on -- is the
// one no delivered generator could produce, so a list holding only inline spellings would re-create
// the blind spot exactly.
//
// The counts are read off the rows: six single-line spellings (three quotings crossed with the
// optional blank before the colon) plus the two explicit keys written wholly on one line are inline;
// the two remaining explicit keys, the one whose value sits below its indicator, and the flow
// mapping split across lines are not.
func TestTheGrammarGridWritesBothKeyColonPlacements(t *testing.T) {
	const wantInline, wantSplit = 8, 4

	inline, split := 0, 0
	for _, spelling := range keySpellingsTheGrammarPermits {
		if writesTheKeyAndItsColonOnOneLine(spelling) {
			inline++
			continue
		}
		split++
	}

	if inline == 0 || split == 0 {
		t.Fatalf("%d spellings write the colon on the key's line and %d write it elsewhere; a list "+
			"holding only one placement re-creates the blind spot", inline, split)
	}
	if inline != wantInline || split != wantSplit {
		t.Errorf("the grammar list holds %d inline and %d split spellings, want %d and %d; "+
			"re-derive the counts from the rows before changing this",
			inline, split, wantInline, wantSplit)
	}
}

// TestASpellingWhoseColonIsOnALaterLineOpensASensitiveBlock reaches the fallback's own verdict for
// the class the grid found leaking, rather than only through a rendered document. It asserts what
// the loop acts on, so a repair that renders correctly for some other reason still fails here.
//
// The claim is per line and specific: the line carrying the value indicator must govern both itself
// and the block below it. That second half is what the reproduction turned on -- the value indicator
// opened no block, so a value written beneath it rather than beside it was rendered in full.
func TestASpellingWhoseColonIsOnALaterLineOpensASensitiveBlock(t *testing.T) {
	for _, spelling := range keySpellingsTheGrammarPermits {
		if writesTheKeyAndItsColonOnOneLine(spelling) {
			continue
		}
		t.Run(spelling.name, func(t *testing.T) {
			indicators := 0
			for _, line := range declarationLinesOf(spelling) {
				content, _ := pastIndentation(line)
				if !strings.HasPrefix(content, explicitKeyValueIndicator) {
					continue
				}

				indicators++
				if _, governs := governanceOf(line, content); governs != thisLineAndTheBlockBelow {
					t.Errorf("the fallback reads %q as governing %v, so the value it carries (%s) "+
						"and the block beneath it are rendered as written", line, governs, spelling.clause)
				}
			}
			if indicators != 1 {
				t.Fatalf("this spelling writes %d value-indicator lines; the assertion above is "+
					"about that line, so a row without one asserts nothing", indicators)
			}
		})
	}
}

// TestAFlowMappingWhoseImplicitKeyIsSplitFromItsColonIsContained is the one spelling of this class
// the grid cannot run on both branches.
//
// YAML 1.2 permits a line break as separation inside a flow mapping (§7.4, §6.1), so `{secrets` / `:
// value}` is a legal implicit key split from its colon. This parser refuses it -- measured, and
// pinned below rather than assumed, because if a later version accepts it the document starts taking
// the path-aware branch and this test would otherwise keep asserting the fallback's behaviour under
// the other branch's name.
func TestAFlowMappingWhoseImplicitKeyIsSplitFromItsColonIsContained(t *testing.T) {
	secret := secretMarkedOnEveryLine("plain")
	body := signingSecrets(grammarMargin + "{" + sensitiveKeyOfTheGrammarGrid + "\n" +
		grammarMargin + ": " + onOneLine(secret.text) + "}\n")
	planted := plantedOnTheFallback(
		"a flow mapping whose implicit key is split from its colon", body, secret.markers)

	if ran, pathAware := branchTaken(planted); pathAware {
		t.Fatalf("this parser now reads the split implicit key, so the document takes the %s branch; "+
			"add the spelling to keySpellingsTheGrammarPermits, where both branches run it:\n%s",
			ran, planted.text)
	}
	assertNothingLeaks(t, planted)
}

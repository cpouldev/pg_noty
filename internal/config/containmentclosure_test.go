package config

import (
	"strings"
	"testing"
)

// This file closes the containment grid's generated dimensions over the production enumerations they
// have to cover. A generator missing an arm is not a smaller property: it reads as universal while
// excluding exactly the case that arm was written for, and the green run is then taken for coverage
// of it.
//
// Every count below is asserted by equality against the enumeration itself rather than against a
// number copied beside it, so an arm added to the production code fails here by name before it can
// fail as an unreached branch. A prose note asking a later author to remember is the mechanism this
// replaces -- it was tried for two rounds and did not hold.
//
// Which enumeration is the authority depends on what the code under test *is*. A switch over the node
// shapes the parser produces is itself the contract, so the generator is closed over the code's own arms.
// A regex standing in for YAML's key grammar is a hypothesis about that contract, so its generator is
// closed over the grammar instead -- closing it over the regex made the one gap it existed to report
// unreportable.

// TestEveryFlowDelimiterTheRedactorCountsIsWrittenIntoASecret closes secretTails over the
// delimiters sensitiveContinuation switches on. Each is asserted separately, because a tail holding
// three of them and none of the fourth would satisfy any "some tail holds a delimiter" check.
func TestEveryFlowDelimiterTheRedactorCountsIsWrittenIntoASecret(t *testing.T) {
	for _, delimiter := range flowDelimitersTheStateMachineCounts {
		if !anySecretTailHolds(delimiter) {
			t.Errorf("no generated secret holds %q, which sensitiveContinuation branches on, so that "+
				"arm is unreachable from every seed and every mutation of one", delimiter)
		}
	}
}

// flowDelimitersTheStateMachineCounts are the bytes sensitiveContinuation.afterFrom has a case for,
// together with the comment indicator that ends its lexical line. The quotes are listed too, since a
// tail holding one exercises the arm that must *not* treat a delimiter inside a quoted scalar as one.
var flowDelimitersTheStateMachineCounts = []string{"[", "]", "{", "}", ",", `"`, "'", commentIndicator}

func anySecretTailHolds(text string) bool {
	for _, tail := range secretTails {
		if strings.Contains(secretMarkedOnEveryLine(tail).text, text) {
			return true
		}
	}
	return false
}

// TestTheInlineKeyDimensionIsExactlyWhatTheGrammarWritesBesideAValue closes keySpellings over YAML's
// spellings rather than over sensitiveKeyForms's own alternatives.
//
// The test this replaces derived its expected count from `len(keyQuotings) * len(keySpacings)` --
// from the matcher under test. A spelling the matcher never matched could therefore never fail it,
// which is how YAML's explicit-key form survived four reviews and some ten million generated
// executions while a database password and two signing secrets rendered verbatim.
//
// keySpellings is the key dimension the *layout* grid crosses, and a layout pastes a key form in
// front of a value it writes itself, so what this dimension can express is exactly the spellings the
// grammar writes beside their value. The rest -- the explicit-key introducer, and every spelling
// whose colon is on a later line -- are run whole by the grammar grid instead. Both directions are
// asserted, so a form here the grammar does not permit fails as loudly as a spelling it permits and
// this dimension omits.
func TestTheInlineKeyDimensionIsExactlyWhatTheGrammarWritesBesideAValue(t *testing.T) {
	permitted := map[string]string{}
	for _, spelling := range keySpellingsTheGrammarPermits {
		permitted[spelling.write(sensitiveKeyOfTheGrammarGrid, grammarProbeValue)] = spelling.name
	}

	generated := 0
	for _, form := range keySpellings {
		if _, admits := permitted[writtenInline(form)]; !admits {
			t.Errorf("the inline key dimension writes %q, which no spelling in the grammar list "+
				"permits; this dimension has to be a subset of YAML's spellings, not of the regex's",
				writtenInline(form))
			continue
		}
		generated++
	}

	besideItsValue := 0
	for _, spelling := range keySpellingsTheGrammarPermits {
		if writesTheKeyBesideItsValue(spelling) {
			besideItsValue++
		}
	}
	if generated != besideItsValue {
		t.Errorf("the inline key dimension writes %d of the %d spellings the grammar writes beside "+
			"their value; the layouts cross this dimension, so an omitted one is a spelling no "+
			"layout ever plants", generated, besideItsValue)
	}
}

// TestTheFallbackGovernsEveryInlineSpellingTheGrammarPermits is the matcher's own half of the same
// claim, asserted against the verdict the fallback loop acts on rather than through a document.
func TestTheFallbackGovernsEveryInlineSpellingTheGrammarPermits(t *testing.T) {
	for _, spelling := range keySpellingsTheGrammarPermits {
		if !writesTheKeyAndItsColonOnOneLine(spelling) {
			continue
		}
		line := strings.TrimSuffix(spelling.write(sensitiveKeyOfTheGrammarGrid, grammarProbeValue), "\n")

		content, _ := pastIndentation(line)
		if _, governs := governanceOf(line, content); governs != theValueOnThisLine {
			t.Errorf("the fallback reads %q (%s) as governing %v, so a secret written there is a "+
				"secret it was never asked about", line, spelling.clause, governs)
		}
	}
}

// TestEveryGeneratedLayoutFamilyReachesTheGrid pins the whole of secretLayouts against its parts, so
// a family dropped from generatedContainmentLayouts -- or a table that grew without its family --
// fails here rather than silently narrowing every property quantified over the grid.
func TestEveryGeneratedLayoutFamilyReachesTheGrid(t *testing.T) {
	families := map[string]int{
		"fallback continuation":      len(fallbackSensitiveContinuationLayouts()),
		"dedented continuation":      len(dedentedContinuationLayouts()),
		"post-close tail":            len(closeTailsThatEndAValue),
		"unreadable key":             len(unreadableKeySpellings),
		"unreadable key value start": len(valueStartsAfterAnUnreadableKeysColon),
		"inner-line opener": len(openersALineCanLeaveOpen) *
			len(positionsAnOpenerCanBeWrittenAt),
		"written value shape": len(writtenValueShapes),
		// One per position whose contract hides a container holding a sensitive leaf, plus the
		// undeclared control. Which positions are left out is pinned separately, by
		// TestTheSchemaPositionDimensionLeavesOutOnlyItsThreeKnownReasons.
		"schema position": len(schemaPositionLayouts()),
		"block opener gap": len(openerGapMaterial) *
			len(openerGapContainers),
	}

	generated := 0
	for name, rows := range families {
		if rows == 0 {
			t.Errorf("the %s family generates no layout at all", name)
		}
		generated += rows
	}
	if got := len(generatedContainmentLayouts()); got != generated {
		t.Fatalf("generatedContainmentLayouts returns %d layouts, want the %d its families declare",
			got, generated)
	}

	const writtenOut = 13
	if got := len(secretLayouts); got != writtenOut+generated {
		t.Errorf("secretLayouts holds %d layouts, want %d written out plus %d generated",
			got, writtenOut, generated)
	}
}

// TestEveryLayoutNameIsDistinct keeps the grid's subtest names -- and every failure message that
// quotes one -- able to identify the row that failed.
func TestEveryLayoutNameIsDistinct(t *testing.T) {
	seen := make(map[string]bool, len(secretLayouts))
	for _, layout := range secretLayouts {
		if seen[layout.name] {
			t.Errorf("two containment layouts are both named %q", layout.name)
		}
		seen[layout.name] = true
	}
}

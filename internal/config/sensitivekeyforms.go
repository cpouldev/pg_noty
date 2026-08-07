package config

import (
	"regexp"
	"slices"
	"strings"
)

// This file is the package's one answer to "does a sensitive key name govern these bytes".
//
// It answers it for both of D3's branches, because the branches disagreeing about it is what leaked.
// The path-aware branch reads the parser's decoded key name, so its half is namesASensitiveKey and
// nothing more -- the parser has already resolved the spelling. The fallback stands in for that
// parser over raw text, so its half is governanceOf, which has to know every spelling YAML permits
// itself. Both halves read the same names, declared as data in schema.go, and both are reconciled
// against the grammar rather than against each other by
// TestBothBranchesContainASecretWrittenInEverySpellingTheGrammarPermits -- a twelve-spelling grid
// run on both branches, whose authority is YAML 1.2 and not this file.
//
// Before that reconciliation existed the question had four owners that could each independently
// answer "no": this file's regex, leadingkey.go's token classification, sensitivekeys.go's
// keptVerbatim, and schemawalk.go's own membership test. They drifted in three consecutive rounds,
// most recently over YAML's explicit-key form, which writes the name on one line and the colon that
// separates it from its value on the next.

// keyBoundary is what a key may begin after: the start of the line, indentation, or the
// punctuation that opens or separates a flow collection or a sequence item. It is what keeps
// `listen_url` from matching as `url` -- that key matches under its own name instead.
const keyBoundary = `(?:^|[\s{\[,-])`

// sensitiveKeyForms matches each sensitive key name in the spellings that write the name and the
// colon separating it from its value on one line: bare, single-quoted, double-quoted, and with
// whitespace before the colon, in any combination. The forms are derived from the same table the
// names are, so a newly declared sensitive key is recognised in all of them the moment it is
// declared. The spellings that write the colon elsewhere are governanceOf's other arm.
var sensitiveKeyForms = compiledKeyForms(sensitiveKeyNames)

func compiledKeyForms(names []string) []*regexp.Regexp {
	forms := make([]*regexp.Regexp, 0, len(names))
	for _, name := range names {
		// `[ \t]*` rather than `\s*`: the scan reads one line, so the only whitespace that
		// can sit before the colon is the whitespace a line is written with.
		forms = append(forms, regexp.MustCompile(keyBoundary+`['"]?`+regexp.QuoteMeta(name)+`['"]?[ \t]*:`))
	}
	return forms
}

// namesASensitiveKey reports whether a leaf key name is one the schema declares sensitive. It is
// the path-aware branch's whole half of this question: that branch is handed a name the parser
// decoded, so every spelling has already collapsed into one by the time it asks.
func namesASensitiveKey(name string) bool {
	return slices.Contains(sensitiveKeyNames, name)
}

// keyGovernance is what a sensitive key name governs at one line of a document the parser rejected.
type keyGovernance uint8

const (
	// noSensitiveKeyGoverns -- no spelling of a sensitive name reaches this line's bytes, so
	// whether the line may be rendered as written is the line-shape axis's question, not this one.
	noSensitiveKeyGoverns keyGovernance = iota
	// theValueOnThisLine -- the name and the colon separating it from its value are both written
	// here, so what the name governs begins past that colon and runs to the end of the line.
	theValueOnThisLine
	// thisLineAndTheBlockBelow -- YAML's explicit-key value indicator opens the value of a key
	// written on an earlier line, so both this line and the block it opens are that key's value.
	thisLineAndTheBlockBelow
)

// governanceOf reports what a sensitive key name governs on one line, and the byte offset within
// line at which the bytes it governs begin.
func governanceOf(line, content string) (byteOffset, keyGovernance) {
	// Asked before the single-line spellings because it governs strictly more: the whole of this
	// line and every line of the block beneath it, where a name written with its own colon governs
	// only what follows that colon. A line writing both -- `: url: <value>` -- is the value half of
	// a key written elsewhere, and the reading that hides more is the one that answers
	// (TestALineWritingBothSpellingsIsGovernedByTheReadingThatHidesMore).
	if opensAnExplicitKeysValue(content) {
		return byteOffset(len(line) - len(content)), thisLineAndTheBlockBelow
	}
	if at, found := earliestSensitiveKeyEnd(line); found {
		return at, theValueOnThisLine
	}
	return 0, noSensitiveKeyGoverns
}

// explicitKeyValueIndicator introduces the value half of a key written in YAML's explicit-key form:
// `? name` on one line and `: value` on a later one (YAML 1.2 §7.4.2).
const explicitKeyValueIndicator = ":"

// opensAnExplicitKeysValue reports whether a line's content begins with that indicator.
//
// Which key the value belongs to is deliberately not asked. That name is on another line and may be
// written in a form no raw-text scan can decode -- `? !!str url` names a sensitive key and spells
// none of it -- so a refusal conditioned on reading the name is a refusal the document can switch
// off, which is the shape of guard this branch has now been burned by twice.
//
// Nothing is given up by answering every such line the same way. Content opening with a colon is
// never a key of its own, because a name is at least one character; it is a value, or it is text a
// parser rejected, and this branch reads nothing else.
func opensAnExplicitKeysValue(content string) bool {
	return strings.HasPrefix(content[sequenceMarkerWidth(content):], explicitKeyValueIndicator)
}

// earliestSensitiveKeyEnd is where the value begins on a line whose key is a sensitive one: just
// past the colon of whichever sensitive key on it ends earliest, so a flow mapping holding one is
// caught as well as a block one. The answer is a byte offset, because it comes from a scan over
// the line's text rather than from the package's column derivation.
//
// The earliest end is what makes the answer safe rather than merely deterministic: blanking runs
// to the end of the line, so starting at the earliest key takes every value after it too. A line
// holding two sensitive keys therefore cannot leak the first, and the answer does not depend on
// the order the names come in.
func earliestSensitiveKeyEnd(line string) (byteOffset, bool) {
	end, found := byteOffset(0), false

	for _, form := range sensitiveKeyForms {
		at := form.FindStringIndex(line)
		if at == nil {
			continue
		}
		if past := byteOffset(at[1]); !found || past < end {
			end, found = past, true
		}
	}
	return end, found
}

// blankValueAfter replaces the value written after a byte offset with the placeholder. A line
// with nothing but whitespace after that offset is returned unchanged: a key whose value is
// written on the lines below has nothing on this one to hide, a placeholder here would stand
// where a mapping or a list begins, and the lines that value does occupy are redacted as the
// block they are.
//
// at is the end of a match against this very line, so it cannot point past it; a line ending
// exactly there slices to the empty string and is answered by the same clause.
func blankValueAfter(line string, at byteOffset) string {
	if strings.TrimSpace(line[at:]) == "" {
		return line
	}
	return line[:at] + " " + redactionPlaceholder
}

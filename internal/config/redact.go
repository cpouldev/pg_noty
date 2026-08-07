package config

import (
	"regexp"

	"github.com/goccy/go-yaml/ast"
)

// This file is the choke point of secret containment, and the whole of what containment means.
// Every renderer works by quoting the original source bytes around a position, and a slice of
// source has no idea what it means -- a signing secret quotes exactly as faithfully as a
// mistyped key -- so safety has to be layered on before the quoting, not after (skill Pattern
// 11).
//
// It decides what replaces a secret, which branch answers for a document, and how a rune column
// is found in a line -- the three things both branches need, which is why the column primitives
// live here rather than beside one caller. Where the secrets are is answered by sensitivepaths.go
// while the document parses and by sensitivekeys.go when it does not, which are D3's two branches.
// Sensitivity itself is decided nowhere here: it is declared as data in schema.go, so a newly
// sensitive key needs no change to any of these files and none at all to the renderer.

// redactionPlaceholder is what a secret is replaced by.
//
// GENUINE DIVERGENCE from skill Pattern 11, whose example placeholder is «redacted». This
// package uses the ASCII form for golden-file stability: the rendered text of some ninety
// fixtures is compared byte for byte, and a placeholder built from multi-byte characters makes
// every one of those comparisons depend on the editor, terminal and diff tool that touches the
// file, while contributing nothing to containment -- the secret's bytes are gone either way.
// Returning to the skill's placeholder would mean re-recording every golden that holds one, and
// accepting that the byte length every tool outside Go reports for such a line no longer
// matches the rune columns this package counts in.
//
// It is a named constant, referenced by the redactor and by its tests alike, so the divergence
// cannot drift into two spellings.
const redactionPlaceholder = "[redacted]"

// redactedLines is the render-only copy of the configuration text: one entry per source line,
// with every value the schema declares sensitive replaced and any resulting column shifts carried.
// It is the only way rendered text
// reaches source text, which is what makes containment structural rather than a habit (ADR-5,
// skill Pattern 11: redact before extraction, never scrub after).
//
// Its signature is the ordering guarantee. It takes the source bytes and returns lines, so it
// cannot be handed an assembled block: the post-hoc scrub ADR-5 prohibits would not type-check
// as a call to it (TestRedactionCannotBeAppliedToFinishedText).
//
// D3's two branches. While the document parses, sensitivity is resolved by path, so database URL
// values keep everything but credentials, destination URLs stay public, and signing secrets go
// wholesale. When it does not parse there are no paths, and sensitivity falls back to key names,
// which over-redacts and never under-redacts.
func redactedLines(src []byte) quotableLines {
	// The display name is unused: every diagnostic being rendered carries its own File, and
	// the diagnostics this parse produces are discarded -- it runs only to find secrets.
	text := newSource("", src)

	// The same entry point the loader parses through, so a position this resolves cannot drift
	// from one a diagnostic carries (TestOnlyParseGoParsesTheDocument).
	root, diags := parseDocument(text)

	// Neither branch is handed a slice to write. The fallback builds its answer line by line and
	// the path-aware branch takes its own renderCopy, so the source's lines are still the bytes a
	// later render quotes whichever branch ran (TestNeitherRedactionBranchWritesTheSourcesLines).
	// What was withheld travels with the lines, because a diagnostic's own message and hint are a
	// second channel of rendered text and have to be held to the same answer (diagnostictext.go).
	return withEveryWithheldWord(text, redactedBranch(text, root, diags))
}

// redactedBranch is whichever of D3's two branches this document takes.
func redactedBranch(text *source, root ast.Node, diags Errors) quotableLines {
	if !pathsAreResolvable(root, diags) {
		return newQuotableLines(redactBySensitiveKeyName(text.lines, text.continuesPhysicalLine))
	}
	return redactDeclaredPaths(text, root)
}

// withEveryWithheldWord records on the redacted lines what producing them removed.
func withEveryWithheldWord(text *source, lines quotableLines) quotableLines {
	return lines.withholding(withheldWords(text.lines, lines.text))
}

// pathsAreResolvable is D3's branch condition, named once so that the branch a document takes is
// one decision rather than several. A path can be resolved to a line only for a document the
// parser accepted, that stage C found a root mapping in, and whose lines the parser and this
// package number the same way. Everything else falls back to key names, which is the branch that
// reads the very lines a renderer quotes and therefore cannot disagree with them.
// It no longer excludes a document holding a lone carriage return, and the argument that retired that
// exclusion was **wrong twice** -- recorded here rather than deleted, because the reasoning is the thing
// to avoid, not the sentence.
//
// The claim was "after the fix no *line* can hold a carriage return, so the guard could never fire". That
// is a fact about `source.lines`, the normalised view. The exclusion's predicate ranged over the
// *document*, whose bytes still hold every return there ever was, so the proof was about a different
// value than the guard read. Two
// secret leaks followed, one on each redaction branch, and restoring the exclusion blanked both.
//
// What makes the removal sound now is not that argument but the repair beneath it: a lone return is
// marked at construction (source.go's carriageReturnSplits), both branches consult the marking, and the
// two classes are closed by named reproductions -- a block scalar split by a return, and a value ending
// exactly at one. The exclusion would today only over-redact documents both branches already handle.
func pathsAreResolvable(root ast.Node, diags Errors) bool {
	return len(diags) == 0 && root != nil
}

// redactedText is what replaces a sensitive value's source text when the whole of that text goes.
//
// A bare environment reference is returned unchanged, because the bytes on disk are the
// reference and never what it resolves to. AC #24 requires the reference to stay visible: a
// diagnostic that hid it could not say which variable is at fault, while hiding it would
// protect nothing that is written down.
//
// It takes no sensitivity: a declaration decides which *extent* goes, and this decides only what
// replaces one. It used to take a kind and dispatch a urlPassword to redactedConnectionString --
// an arm neither call site in redactiongeometry.go could reach, since one passes entireValue by
// name and the other sits past that file's own urlPassword branch. A parameter with one reachable
// value reads as a choice this function still makes.
func redactedText(text string) string {
	if isBareEnvReference(text) {
		return text
	}
	return redactionPlaceholder
}

// redactedConnectionString replaces every password inside a connection string and leaves the rest
// of it -- scheme, user, host, port and database name -- legible, because a URL blanked
// wholesale cannot tell its own diagnostic what it rejected (D3). Where the spans sit is
// connstring.go's answer; that they are replaced at all is this file's decision.
//
// Text carrying no password is returned unchanged, which is how `url: ${DATABASE_URL}` renders
// as the reference it is, and how a password-less URL stays whole. So is a password that is
// itself a bare reference: what is written down there is the reference, not the secret.
//
// Every span is replaced rather than the first one found, because one string can carry a password
// in more than one place. They arrive right to left, so each replacement leaves the offsets of the
// ones still to come exactly where they were derived.
func redactedConnectionString(text string) string {
	redacted, _ := redactedConnectionStringSpans(text)
	return redacted
}

// placeholderRunes is how wide the placeholder is in the rune columns every caret is counted in.
// redactiongeometry.go reads it to place a caret after a password replacement, and reads it by
// this name because only the files owning a containment output may name the placeholder itself
// (TestRedactionCannotBeAppliedToFinishedText). It is one name rather than a field on each
// replaced span: there is one placeholder, so a per-element width was this same value every time
// while reading as though a replacement could be some other width.
var placeholderRunes = runeCount(redactionPlaceholder)

// redactedConnectionStringSpans is the redacted text and the source spans it replaced, which the
// caret geometry needs and redactedConnectionString discards.
func redactedConnectionStringSpans(text string) (string, []span) {
	redacted := text
	var replaced []span

	for _, password := range passwordSpans(text) {
		if isBareEnvReference(text[password.from:password.to]) {
			continue
		}
		redacted = redacted[:password.from] + redactionPlaceholder + redacted[password.to:]
		replaced = append(replaced, password)
	}
	return redacted, replaced
}

// bareEnvReference matches a reference carrying no default: `${`, a name, `}`. Step 3 owns the
// interpolation grammar; this asks only the narrower question of whether a value can hold a
// secret at all. One carrying a default is not one of these: `${NAME:-x}` supplies written
// text, and written text can be a secret.
//
// It is built from that grammar's own literals rather than spelled a second time. A private
// charset would be a second definition of "a variable name", free to disagree with the first
// -- and it did: `[^${}:]+` accepted `${1abc}` and `${a-b}`, which the grammar refuses, so a
// password written as either was taken for a reference and left in the clear.
var bareEnvReference = regexp.MustCompile(
	`^` + regexp.QuoteMeta(referenceOpen) + variableNamePattern + regexp.QuoteMeta(referenceClose) + `$`)

func isBareEnvReference(text string) bool {
	return bareEnvReference.MatchString(text)
}

// blankFromColumn replaces everything from a rune column to the end of the line. Both branches
// write through it, which is what confines the placeholder to this file and to the fallback's own
// (TestRedactionCannotBeAppliedToFinishedText).
func blankFromColumn(line string, start columnInRunes) string {
	return line[:byteOffsetOf(line, start)] + redactionPlaceholder
}

// replaceRunes replaces count runes of line, starting at the rune column start.
//
// A span reaching past the line's last rune takes the rest of the line with it rather than
// stitching a tail back on. It cannot occur at its one call site -- two sensitive values on one line
// never overlap, so the rightmost-first order leaves every column to the left where it was derived
// -- and the direction is asserted rather than assumed, because over-redacting is the only answer
// redaction may give (TestReplacingASpanPastTheLastRuneCanOnlyOverRedact).
func replaceRunes(line string, start columnInRunes, count int, replacement string) string {
	from, past := byteSpanOf(line, start, count)

	return line[:from] + replacement + line[past:]
}

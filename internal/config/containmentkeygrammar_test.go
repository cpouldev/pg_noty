package config

// This file enumerates the spellings YAML 1.2 permits for a leaf key, and the shapes it permits for
// that key's value, taking its authority from the specification rather than from the matcher that
// stands in for it.
//
// The authority matters more than the list. sensitiveKeyForms is a regex standing in for YAML's key
// grammar on the branch that reads a document the parser rejected, and the closure test written to
// keep the generated key dimension honest derived its expected count from that regex's own
// alternatives. A spelling the regex never matched could therefore never fail it -- and YAML's
// explicit-key form, written across two lines, went unrecognised through four reviews and some ten
// million generated executions while a database password and two signing secrets rendered verbatim.
//
// Every row cites the clause that permits it, so widening the matcher cannot quietly narrow this
// list, and a reader can check the list against the specification without running anything.

// grammarMargin is the indentation the declaration beneath `signing:` is written at: signingSecrets
// writes that key at four columns, so its block sits at six.
const grammarMargin = "      "

// keySpellingTheGrammarPermits is one way YAML permits the same leaf key to be written.
//
// write returns the whole declaration -- the block beneath `signing:` rather than one line of it --
// because the explicit-key and split-flow spellings put the key and its colon on different lines and
// cannot be expressed as a prefix of one. That is the difference the delivered keySpelling could not
// express, and therefore the difference its grid could not reach.
type keySpellingTheGrammarPermits struct {
	name   string
	clause string
	write  func(key, value string) string
}

// keySpellingsTheGrammarPermits is twelve spellings of one leaf key. Six leave the key implicit
// before its colon, crossing the three quotings with the optional separation the grammar allows
// there. The other six use YAML's explicit-key indicator, which is what permits the colon to sit on
// a later line -- two writing both indicators on one line, three across two lines, and one inside a
// flow mapping, where a line break is ordinary separation.
var keySpellingsTheGrammarPermits = []keySpellingTheGrammarPermits{
	{
		name:   "bare",
		clause: "§7.4.2 implicit block-mapping key",
		write:  writesOneLine("", "", ""),
	},
	{
		name:   "bare with whitespace before the colon",
		clause: "§7.4.2 implicit key, §6.1 separation before the indicator",
		write:  writesOneLine("", "", " "),
	},
	{
		name:   "double-quoted",
		clause: "§7.3.1 double-quoted style as an implicit key",
		write:  writesOneLine(`"`, `"`, ""),
	},
	{
		name:   "double-quoted with whitespace before the colon",
		clause: "§7.3.1 with §6.1 separation before the indicator",
		write:  writesOneLine(`"`, `"`, " "),
	},
	{
		name:   "single-quoted",
		clause: "§7.3.2 single-quoted style as an implicit key",
		write:  writesOneLine(`'`, `'`, ""),
	},
	{
		name:   "single-quoted with whitespace before the colon",
		clause: "§7.3.2 with §6.1 separation before the indicator",
		write:  writesOneLine(`'`, `'`, " "),
	},
	{
		name:   "explicit key on one line",
		clause: "§7.4.2 explicit key, both indicators on one line",
		write:  writesOneLine("? ", "", " "),
	},
	{
		name:   "explicit key on one line, double-quoted",
		clause: "§7.4.2 explicit key with a §7.3.1 name",
		write:  writesOneLine(`? "`, `"`, " "),
	},
	{
		name:   "explicit key on two lines",
		clause: "§7.4.2 explicit key, the value indicator on its own line",
		write:  writesTwoLines("? ", ""),
	},
	{
		name:   "explicit key on two lines, single-quoted",
		clause: "§7.4.2 explicit key on two lines with a §7.3.2 name",
		write:  writesTwoLines(`? '`, `'`),
	},
	{
		// The blast-radius shape: the value indicator carries nothing of its own, so everything
		// the key governs is written on the lines below it.
		name:   "explicit key whose value is written beneath its value indicator",
		clause: "§7.4.2 explicit key, §8.2.2 block mapping value on the following line",
		write: func(key, value string) string {
			return grammarMargin + "? " + key + "\n" +
				grammarMargin + ":\n" + grammarMargin + "  " + value + "\n"
		},
	},
	{
		name:   "a flow mapping whose explicit key and value indicator are on separate lines",
		clause: "§7.4 flow mapping with a §7.4.2 explicit key, §6.1 separation may be a line break",
		write: func(key, value string) string {
			return grammarMargin + "{? " + key + "\n" + grammarMargin + ": " + value + "}\n"
		},
	},
}

// writesOneLine writes the name between two fixtures and its colon after an optional separation, all
// on the declaration's single line.
func writesOneLine(before, after, separation string) func(key, value string) string {
	return func(key, value string) string {
		return grammarMargin + before + key + after + separation + ": " + value + "\n"
	}
}

// writesTwoLines writes YAML's explicit-key indicator and the name on one line and the value
// indicator on the next, which is the spelling neither the matcher nor its closure test could reach.
func writesTwoLines(before, after string) func(key, value string) string {
	return func(key, value string) string {
		return grammarMargin + before + key + after + "\n" + grammarMargin + ": " + value + "\n"
	}
}

// valueShapeTheGrammarPermits is one shape YAML permits for the value beside such a key. Three are
// enough here because the node-shape dimension is the subject of containmentnodeshape_test.go; what
// this grid varies is the *spelling of the key*, and a value shape that reads differently on the two
// branches is what keeps a spelling from being certified by a scalar alone.
type valueShapeTheGrammarPermits struct {
	name   string
	clause string
	write  func(secret string) string
}

var valueShapesTheGrammarPermits = []valueShapeTheGrammarPermits{
	{
		name: "a plain scalar", clause: "§7.3.3 plain style",
		write: func(secret string) string { return onOneLine(secret) },
	},
	{
		name: "a double-quoted scalar", clause: "§7.3.1 double-quoted style",
		write: func(secret string) string { return yamlQuoted(onOneLine(secret)) },
	},
	{
		name: "a flow sequence", clause: "§7.4.1 flow sequence",
		write: func(secret string) string { return "[" + yamlQuoted(onOneLine(secret)) + "]" },
	},
}

// sensitiveKeyOfTheGrammarGrid is the leaf name every cell writes: signing.secrets, whose whole value
// the schema declares sensitive. That is the strongest declaration in the table, so a surviving
// marker is a secret by the table's own account rather than by this grid's.
const sensitiveKeyOfTheGrammarGrid = "secrets"

// plantsSpelling is one cell of the grammar grid on both of D3's branches: the same declaration,
// once in a document that parses and once in one that does not.
func plantsSpelling(
	secret markedSecret,
	spelling keySpellingTheGrammarPermits,
	shape valueShapeTheGrammarPermits,
) []plantedSecret {
	where := shape.name + " beneath a " + spelling.name + " key (" + spelling.clause + ")"
	body := signingSecrets(spelling.write(sensitiveKeyOfTheGrammarGrid, shape.write(secret.text)))

	return plantedOnBothBranches(where, body, secret.markers)
}

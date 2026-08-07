package config

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// seedTails are the hand-written byte classes together with every secret the fuzzer has already
// committed. Crossing the committed ones with the whole grid is what keeps each committed finding
// reachable after secretLayouts grows: the pairing an entry was minted for is reached here by name,
// not by the ordinal the entry stores.
func seedTails() []string {
	tails := slices.Clone(secretTails)
	for _, name := range slices.Sorted(maps.Keys(corpusMintedTails)) {
		tails = append(tails, corpusMintedTails[name])
	}
	return tails
}

// secretTails cross input byte classes and redactor branch literals with every
// layout and key spelling. A plain test run therefore executes the full seed grid.
var secretTails = []string{
	"",
	"plain",
	"holds: a colon",
	`holds "a quote" and 'an apostrophe'`,
	"holds ${an_interpolation}",
	"holds\na newline",
	"holds\ra lone carriage return",
	"holds\r\na carriage return and a newline",
	"holds\ta tab and a trailing space \nbefore a line break",
	"-----BEGIN PRIVATE KEY-----\nMIIBOgIBAAJBA\n-----END PRIVATE KEY-----",
	commentIndicator + "looks like a comment",
	documentStart + "looks like a document start",
	documentEnd + "looks like a document end",
	"holds\n" + commentIndicator + "a second line that looks like a comment",

	// The delimiters the redactor's own scans branch on. A class the code tests for and no generator
	// produces is a branch no run can reach, and the green run then reads as coverage of it.
	// sensitiveContinuation counts all four flow delimiters and stops at a comment;
	// containsUnreadableFlowKey additionally reads the comma that begins a flow mapping's next key.
	// Every one is written inside a secret so that the quoting layouts carry it through a quoted
	// scalar -- where the scan must *not* act on it -- and the raw ones carry it as content.
	"holds ] a flow sequence closer",
	"holds [ a flow sequence opener",
	"holds } a flow mapping closer",
	"holds { a flow mapping opener",
	"holds , a flow entry separator",
	"holds ]} both closers and a , separator",
}

func signingSecrets(declaration string) string {
	return "listeners:\n" +
		"- name: order_paid\n" +
		"  destination:\n" +
		"    signing:\n" +
		declaration
}

func connectionStringHiding(secret, host string) string {
	return "postgres://noty:" + secret + "@" + host + "/noty"
}

func indentedLines(text string, indent int) string {
	margin := strings.Repeat(" ", indent)

	var block strings.Builder
	for _, line := range strings.Split(text, "\n") {
		block.WriteString(margin + line + "\n")
	}
	return block.String()
}

// yamlQuoted preserves raw carriage returns because they are a parser line-break
// class the containment grid is required to reach.
func yamlQuoted(text string) string {
	return strings.ReplaceAll(strconv.Quote(text), `\r`, "\r")
}

func quotedScalarBody(text string) string {
	quoted := yamlQuoted(text)
	return quoted[1 : len(quoted)-1]
}

func quoteWithoutClosing(text string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(text)
}

// singleQuoteWithoutClosing is quoteWithoutClosing for YAML's other quote grammar, which escapes by
// doubling rather than with a backslash. Both exist so a layout can write a generated secret inside
// an opener the secret's own bytes cannot close -- a row whose payload closes its opener no longer
// plants the shape it is named for.
func singleQuoteWithoutClosing(text string) string {
	return strings.ReplaceAll(text, "'", "''")
}

// onOneLine folds a planted secret onto one physical line, keeping every other byte class it holds.
// A layout whose shape depends on what follows its opener on that line needs it: without it a
// generated line break moves the rest of the secret to a line the row never reasoned about, and the
// row plants a shape other than the one it is named for. The markers survive the fold, which is what
// the oracle searches for. It reads the package's own break set, so every break class secretTails
// enumerates is folded.
func onOneLine(text string) string {
	var built strings.Builder
	for number, line := range linesWithTheirBreaks(text) {
		if number > 0 {
			built.WriteString(" ")
		}
		built.WriteString(line.content)
	}
	return built.String()
}

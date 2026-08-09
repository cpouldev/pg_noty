package config

import "strings"

// This file answers where a spelling puts a key relative to its colon and its value.
//
// It is separate from the spelling table because the table states what YAML permits, while these
// predicates classify those rows -- and the classification is what the closure tests reconcile the
// generated dimensions against. Keeping the two apart is what stops a closure test from being
// written against the classifier that a row's own name would otherwise supply.

// writesTheKeyAndItsColonOnOneLine reports whether a spelling puts the name and the colon that
// separates it from its value on the same line -- which is the class sensitiveKeyForms matches.
func writesTheKeyAndItsColonOnOneLine(spelling keySpellingTheGrammarPermits) bool {
	declaration := spelling.write(sensitiveKeyOfTheGrammarGrid, grammarProbeValue)
	for _, line := range strings.Split(declaration, "\n") {
		if strings.Contains(line, sensitiveKeyOfTheGrammarGrid) {
			return strings.Contains(line[strings.Index(line, sensitiveKeyOfTheGrammarGrid):], ":")
		}
	}
	return false
}

// explicitKeyIndicator opens a key YAML 1.2 §7.4.2 writes out in full, rather than leaving it
// implicit before its colon.
const explicitKeyIndicator = "?"

// writesAnExplicitKeyIndicator reports whether a spelling introduces its name with that indicator.
func writesAnExplicitKeyIndicator(spelling keySpellingTheGrammarPermits) bool {
	for _, line := range strings.Split(spelling.write(sensitiveKeyOfTheGrammarGrid, grammarProbeValue), "\n") {
		if content, _ := pastIndentation(line); strings.HasPrefix(content, explicitKeyIndicator) {
			return true
		}
	}
	return false
}

// writesTheKeyBesideItsValue reports whether a spelling can be written as a key form pasted in front
// of its value, which is what a containment *layout* does with keySpellings. A spelling using the
// explicit-key indicator cannot: the indicator precedes the name, so the key is not a prefix of the
// pair. Those are covered by the grammar grid, which writes whole declarations instead.
func writesTheKeyBesideItsValue(spelling keySpellingTheGrammarPermits) bool {
	return writesTheKeyAndItsColonOnOneLine(spelling) && !writesAnExplicitKeyIndicator(spelling)
}

// grammarProbeValue stands where a value would be when a test measures the *shape* of a declaration
// rather than what happens to its bytes.
const grammarProbeValue = "value"

// writtenInline is the declaration one inline key form produces for the grammar grid's key and probe
// value, so the two dimensions can be compared as the bytes they write rather than by their names.
func writtenInline(form keySpelling) string {
	return grammarMargin + form.write(sensitiveKeyOfTheGrammarGrid) + " " + grammarProbeValue + "\n"
}

// declarationLinesOf is the non-blank lines one spelling writes, so a per-line assertion can name the
// line it failed on rather than the whole block.
func declarationLinesOf(spelling keySpellingTheGrammarPermits) []string {
	var lines []string
	for _, line := range strings.Split(spelling.write(sensitiveKeyOfTheGrammarGrid, grammarProbeValue), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

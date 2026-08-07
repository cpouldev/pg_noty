package config

import "strings"

// This file wires three dimensions into the containment grid that were previously private to the
// unit tests that introduced them, so the fuzzer can vary them against every key spelling and every
// generated secret rather than only against the one fixture each unit test wrote.
//
// Each family is generated from the very table its unit test ranges over, not from a copy, so the
// two cannot drift and a row added there arrives here with it. The counts are pinned in
// containmentclosure_test.go, so a table that grows without its layouts fails by name.

// dedentedContinuationLayouts hold a secret whose continuation line is indented *less* than the line
// opening it and still more than the block enclosing it. That is what YAML permits a multi-line flow
// scalar, and the path-aware branch's indentation-only walk stopped at the first such line and
// rendered everything after it verbatim.
func dedentedContinuationLayouts() []secretLayout {
	return []secretLayout{
		{
			name: "a double-quoted scalar whose continuation dedents below its opener",
			body: func(secret string, key keySpelling) string {
				escaped := quotedScalarBody(secret)
				return signingSecrets("      " + key.write("secrets") + ` "` + escaped + "\n" +
					"     " + escaped + "\"\n")
			},
		},
		{
			name: "a flow sequence whose element dedents below its opener",
			body: func(secret string, key keySpelling) string {
				quoted := yamlQuoted(secret)
				return signingSecrets("      " + key.write("secrets") + " [" + quoted + ",\n" +
					"     " + quoted + "]\n")
			},
		},
	}
}

// postCloseTailLayouts vary what follows a sensitive flow container's closing delimiter, which is
// the byte sensitiveContinuation reads to decide whether the value's provenance carries to the next
// line. The secret is written *before* the closer in every row, so the claim stays "this must
// vanish" whatever the tail turns out to license.
func postCloseTailLayouts() []secretLayout {
	layouts := make([]secretLayout, 0, len(closeTailsThatEndAValue))
	for _, tail := range closeTailsThatEndAValue {
		layouts = append(layouts, secretLayout{
			name:         "a flow sequence closed and followed by " + tail.name,
			fallbackOnly: true,
			body: func(secret string, key keySpelling) string {
				// Indented past the sensitive key's own six columns, because a generated tail may
				// open with the container's own closer and end the flow this layout relies on.
				entry := "      other: " + watchedTailSlot + secret + "]" + tail.text
				return signingSecrets("      "+key.write("secrets")+" [first\n"+
					indentedContinuationLines(entry, "        ")+"\n") + unparseableTail
			},
			watchedTail: textMarkedAtBothEnds("POST-CLOSE", "continued "),
		})
	}
	return layouts
}

// unreadableKeyLayouts write a key whose name only a parser can decode. The fallback blanks such a
// line whole because the name may decode to a sensitive one, and these rows put that decision under
// generated secret bytes and every spelling of the sensitive key above them.
func unreadableKeyLayouts() []secretLayout {
	layouts := make([]secretLayout, 0, len(unreadableKeySpellings))
	for _, unreadable := range unreadableKeySpellings {
		layouts = append(layouts, secretLayout{
			name:         "a secret behind an unreadable " + unreadable.name + " key",
			fallbackOnly: true,
			body: func(secret string, key keySpelling) string {
				return unreadableKeyDocument(key, unreadable.prefix+": "+secret)
			},
		})
	}
	return layouts
}

// unreadableKeyValueStartLayouts vary what follows such a key's colon, which is the other half of
// the same decision. The secret is substituted into the table's own value rather than appended to a
// copy of it, so the shape each row is named for is the shape that gets planted.
func unreadableKeyValueStartLayouts() []secretLayout {
	layouts := make([]secretLayout, 0, len(valueStartsAfterAnUnreadableKeysColon))
	for _, start := range valueStartsAfterAnUnreadableKeysColon {
		layouts = append(layouts, secretLayout{
			name:         "a secret written after an unreadable key's colon, " + start.name,
			fallbackOnly: true,
			body: func(secret string, key keySpelling) string {
				return unreadableKeyDocument(key,
					unreadableKeySpellings[0].prefix+":"+valueStartHiding(start.value, secret))
			},
		})
	}
	return layouts
}

// unreadableKeyDocument writes the unreadable key at the left margin, which is where the table these
// rows are generated from writes it. The indentation is part of each row rather than incidental: a
// row spelling its value as a block scalar indents the value's own lines by two, so a key moved into
// the sensitive block at column seven would put those lines *outside* the value they belong to and
// the row would be planting a secret nowhere the shape it is named for can reach.
func unreadableKeyDocument(key keySpelling, entry string) string {
	return anchorDefinitionLine +
		signingSecrets("      "+key.write("secrets")+" first\n") +
		indentedContinuationLines(entry, "  ") + "\n" + unparseableTail
}

// indentedContinuationLines indents every line of an entry after its first, past the key that entry
// belongs to. A line written at that key's own column is a *sibling entry* rather than part of its
// value, and these layouts plant a secret raw rather than quoted, so a secret holding a line break
// puts its remainder wherever its own bytes fall.
//
// Without this the remainder lands at column one, where -- if those bytes happen to hold `: ` -- it
// reads as a readable key of its own. The fallback keeps such a line by name, deliberately: nothing
// on it is governed by a sensitive key, and blanking every readable key below one would cost every
// diagnostic the context it exists to show. The row would then report a leak against a shape it never
// planted. That decision is pinned by TestTheFallbackKeepsAReadableSiblingOfAnUnreadableKey.
//
// It is the raw families that need this. A layout writing its secret through yamlQuoted or
// indentedLines has already placed every line of it.
//
// The break set is the package's own, so a secret written with any of them is indented here.
func indentedContinuationLines(entry, margin string) string {
	var built strings.Builder
	for number, line := range linesWithTheirBreaks(entry) {
		if number > 0 {
			built.WriteString(margin)
		}
		built.WriteString(line.content + line.ending)
	}
	return built.String()
}

// valueStartHiding puts the secret where the table's row writes its masked value, and after the row
// when it writes none, so no row can plant nothing and pass by holding no marker at all.
func valueStartHiding(value, secret string) string {
	if !strings.Contains(value, hiddenBeneathAKey) {
		return value + secret
	}
	return strings.ReplaceAll(value, hiddenBeneathAKey, secret)
}

// unparseableTail keeps a layout on the fallback branch: an unterminated flow sequence at the root
// is a parse error whatever precedes it.
const unparseableTail = "broken: [\n"

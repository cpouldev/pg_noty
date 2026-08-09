package config

type redactionEdit struct {
	from  columnInRunes
	past  columnInRunes
	runes int
}

// redactionGeometry returns the replacement and the source extents it rewrites. Whole-value
// redactions have one extent; URL redactions use the password spans the connection-string
// grammar found, so caret geometry and secret geometry cannot disagree.
func redactionGeometry(value sensitiveValue) (string, []redactionEdit) {
	if value.kind == urlPassword {
		return connectionStringGeometry(value)
	}

	replacement := redactedText(value.text)
	if replacement == value.text {
		return value.text, nil
	}
	return replacement, []redactionEdit{wholeValueEdit(value, replacement)}
}

// connectionStringGeometry is what a urlPassword declaration replaces, and what it replaces when its
// grammar has nothing to say about the value.
//
// A declaration naming a grammar names one that reads a scalar: a connection string is text. A
// mapping or a list written where one is declared is a shape that grammar cannot read at all, so "no
// password was found in it" is the grammar declining to apply rather than a finding about the text
// -- and returning the value untouched on that answer is a refusal the document switches off by
// choosing its own shape. Such a value goes wholesale, which is the over-redaction D3 prefers to a
// guess, and is what both the key-scoped fallback and the block-written spelling of the same entries
// already did with it.
//
// A scalar keeps everything the grammar can read. That is the other half of D3 and not a hedge: a
// URL blanked whole cannot tell its own diagnostic what it rejected, and `${DATABASE_URL}` has to
// render as the reference it is (AC #24). Both sides are asserted -- the shapes by
// TestAUrlPasswordValueTheConnectionStringGrammarCannotReadIsBlankedWholesale, the bound by
// TestAUrlPasswordScalarKeepsEverythingTheGrammarCanRead.
func connectionStringGeometry(value sensitiveValue) (string, []redactionEdit) {
	if value.writtenAsAContainer {
		// The extent that goes is the whole of the value, which is this file's statement to make;
		// what replaces that extent is redact.go's, so it is asked rather than spelled here and
		// redact.go stays the one place that says what a secret is replaced by.
		whole := redactedText(value.text)
		return whole, []redactionEdit{wholeValueEdit(value, whole)}
	}

	replacement, passwords := redactedConnectionStringSpans(value.text)
	return replacement, passwordEdits(value.text, passwords, value.column)
}

// wholeValueEdit is the extent a replacement of the value's whole text rewrites.
func wholeValueEdit(value sensitiveValue, replacement string) redactionEdit {
	return redactionEdit{
		from:  value.column,
		past:  value.column + columnInRunes(runeCount(value.text)),
		runes: runeCount(replacement),
	}
}

func passwordEdits(text string, passwords []span, start columnInRunes) []redactionEdit {
	edits := make([]redactionEdit, 0, len(passwords))
	for _, password := range passwords {
		before := runeCount(text[:password.from])
		count := runeCount(text[password.from:password.to])
		from := start + columnInRunes(before)
		edits = append(edits, redactionEdit{
			from: from, past: from + columnInRunes(count),
			runes: placeholderRunes,
		})
	}
	return edits
}

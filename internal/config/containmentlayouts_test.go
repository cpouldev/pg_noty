package config

import "strings"

// keySpelling is one YAML spelling of the same leaf key. The path-aware
// branch sees one name; the fallback sees these different byte forms.
type keySpelling struct {
	name  string
	write func(key string) string
}

// keyQuotings and keySpacings are the two alternatives YAML admits around a key's name where it is
// written beside its value: an optional quote on each side, and optional blanks before the colon.
// They are crossed rather than listed, so the layout grid writes every one of those spellings
// instead of the four somebody happened to think of.
//
// What this dimension may hold is checked against the grammar and not against the matcher, by
// TestTheInlineKeyDimensionIsExactlyWhatTheGrammarWritesBesideAValue. The check used to derive its
// expected count from `len(keyQuotings) * len(keySpacings)` -- from the thing under test -- and was
// therefore satisfied by construction, which is how the explicit-key spelling stayed out of every
// generated document for four rounds. The spellings this dimension cannot express, because a layout
// pastes a key form in front of a value, are run whole by the grammar grid in
// containmentkeygrammargrid_test.go.
var (
	keyQuotings = []struct {
		name  string
		quote string
	}{
		{name: "bare"},
		{name: "double-quoted", quote: `"`},
		{name: "single-quoted", quote: `'`},
	}
	keySpacings = []struct {
		name  string
		blank string
	}{
		{name: ""},
		{name: " with whitespace before the colon", blank: " "},
	}
)

var keySpellings = crossedKeySpellings()

func crossedKeySpellings() []keySpelling {
	spellings := make([]keySpelling, 0, len(keyQuotings)*len(keySpacings))
	for _, quoting := range keyQuotings {
		for _, spacing := range keySpacings {
			spellings = append(spellings, keySpelling{
				name: quoting.name + spacing.name,
				write: func(key string) string {
					return quoting.quote + key + quoting.quote + spacing.blank + ":"
				},
			})
		}
	}
	return spellings
}

// secretLayout is one physical shape in which a schema-sensitive value is
// written. watchedTail marks ordinary text that shares a sensitive physical
// extent and therefore must be over-redacted with it.
type secretLayout struct {
	name         string
	body         func(secret string, key keySpelling) string
	fallbackOnly bool
	watchedTail  markedSecret
}

const watchedTailSlot = "PGNOTY-WATCHED-PHYSICAL-TAIL"

var secretLayouts = append([]secretLayout{
	{
		name: "a plain value ending at a carriage return",
		body: func(secret string, key keySpelling) string {
			return signingSecrets("      " + key.write("secrets") + " " +
				strings.ReplaceAll(secret, "\n", " ") + "\r      other: " + watchedTailSlot + "\n")
		},
		watchedTail: textMarkedAtBothEnds("CR-TAIL", "kept"),
	},
	{
		// This malformed document exercises only the fallback. The key-looking
		// continuation is scalar content until the opening quote closes.
		name:         "an unclosed quoted scalar with a key-shaped continuation",
		fallbackOnly: true,
		body: func(secret string, key keySpelling) string {
			return signingSecrets("      " + key.write("secrets") + " \"\n" +
				"      other: " + watchedTailSlot + quoteWithoutClosing(secret) + "\n")
		},
		watchedTail: textMarkedAtBothEnds("QUOTE-TAIL", "continued "),
	},
	{
		name: "a quoted scalar list item",
		body: func(secret string, key keySpelling) string {
			return signingSecrets("      " + key.write("secrets") + "\n" +
				"      - " + yamlQuoted(secret) + "\n")
		},
	},
	{
		name: "a block scalar list item",
		body: func(secret string, key keySpelling) string {
			return signingSecrets("      " + key.write("secrets") + "\n" +
				"      - |\n" + indentedLines(secret, 8))
		},
	},
	{
		name: "a double-quoted scalar written across two lines",
		body: func(secret string, key keySpelling) string {
			escaped := quotedScalarBody(secret)
			return signingSecrets("      " + key.write("secrets") + "\n" +
				`      - "` + escaped + "\n" +
				"          " + escaped + "\"\n")
		},
	},
	{
		name: "a flow sequence holding two secrets",
		body: func(secret string, key keySpelling) string {
			quoted := yamlQuoted(secret)
			return signingSecrets("      " + key.write("secrets") + " [" + quoted + ", " + quoted + "]\n")
		},
	},
	{
		name: "a sensitive key beneath an undeclared parent",
		body: func(secret string, key keySpelling) string {
			return "databse:\n  " + key.write("url") + " " +
				yamlQuoted(connectionStringHiding(secret, "db.internal")) + "\n"
		},
	},
	{
		name: "a sensitive key written on a sequence-item line",
		body: func(secret string, key keySpelling) string {
			return "database:\n- " + key.write("url") + " " +
				yamlQuoted(connectionStringHiding(secret, "db.internal:5432")) + "\n"
		},
	},
	{
		name: "a value written below a sensitive key",
		body: func(secret string, key keySpelling) string {
			return "database:\n  " + key.write("url") + "\n    " +
				yamlQuoted(connectionStringHiding(secret, "db.internal:5432")) + "\n"
		},
	},
	{
		name: "a flow mapping holding a sensitive key",
		body: func(secret string, key keySpelling) string {
			return "database: {" + key.write("url") + " " +
				yamlQuoted(connectionStringHiding(secret, "db.internal:5432")) + "}\n"
		},
	},
	{
		name: "the second sensitive connection string",
		body: func(secret string, key keySpelling) string {
			return "database:\n  " + key.write("listen_url") + " " +
				yamlQuoted(connectionStringHiding(secret, "replica.internal")) + "\n"
		},
	},
	{
		name: "a URI whose query keyword holds the password",
		body: func(secret string, key keySpelling) string {
			return "database:\n  " + key.write("url") + " " +
				yamlQuoted("postgres://noty:decoy@db.internal:5432/noty?sslpassword="+secret) + "\n"
		},
	},
	{
		name: "a keyword connection string whose password repeats",
		body: func(secret string, key keySpelling) string {
			return "database:\n  " + key.write("url") + " " +
				yamlQuoted("host=db.internal password=decoy password="+secret) + "\n"
		},
	},
}, generatedContainmentLayouts()...)

// generatedContainmentLayouts are the layout families built from another table rather than written
// out here. Keeping them in one list is what lets TestEveryGeneratedLayoutFamilyReachesTheGrid
// reconcile each family against the table it is generated from.
func generatedContainmentLayouts() []secretLayout {
	var layouts []secretLayout
	for _, family := range []func() []secretLayout{
		fallbackSensitiveContinuationLayouts,
		dedentedContinuationLayouts,
		postCloseTailLayouts,
		unreadableKeyLayouts,
		unreadableKeyValueStartLayouts,
		innerLineOpenerLayouts,
		writtenValueShapeLayouts,
		schemaPositionLayouts,
		blockOpenerGapLayouts,
	} {
		layouts = append(layouts, family()...)
	}
	return layouts
}

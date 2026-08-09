package config

import (
	"strings"
	"testing"
)

const (
	anchorDefinitionLine = "key_name: &ordinary_key ordinary\n"
	hiddenBeneathAKey    = "PGNOTYHIDDENBENEATHAKEY"
	publicBeneathAKey    = "PGNOTYPUBLICBENEATHAKEY"
)

type writtenFragment struct {
	name   string
	prefix string
	suffix string
}

var unreadableKeySpellings = []writtenFragment{
	{name: "alias", prefix: "*ordinary_key"},
	{name: "anchor property", prefix: "&ordinary_anchor ordinary"},
	{name: "tag property", prefix: "!!str ordinary"},
	{name: "explicit complex", prefix: "? [ordinary: key] "},
}

var colonSeparatorSpellings = []writtenFragment{
	{name: "no space"},
	{name: "space", prefix: " "},
	{name: "tab", prefix: "\t"},
}

var keyEntryLayouts = []writtenFragment{
	{name: "block"},
	{name: "sequence", prefix: "unknown:\n- "},
	{name: "nested flow", prefix: "ordinary: {nested: {", suffix: "}}"},
}

var valueQuotings = []writtenFragment{
	{name: "plain"},
	{name: "double quoted", prefix: `"`, suffix: `:inside"`},
	{name: "single quoted", prefix: `'`, suffix: `:inside'`},
}

var readableKeySpellings = []writtenFragment{
	{name: "double quoted", prefix: `"public"`},
	{name: "single quoted", prefix: `'public'`},
	{name: "colon inside", prefix: `"public:key"`},
}

var colonContentControls = []struct {
	name string
	text string
}{
	{name: "tagged URL value", text: "!!str http://host:5432/path"},
	{name: "anchored URL value", text: "&public http://host:5432/path"},
	{name: "verbatim tag URI", text: "!<tag:example.com,2026:public> value"},
	{name: "nested explicit value", text: "? [ordinary: value]"},
	{name: "quoted tagged value", text: `!!str "public:key"`},
	{name: "comment text", text: "!!str public # public:key"},
}

func writtenAround(part writtenFragment, content string) string {
	return part.prefix + content + part.suffix
}

func fragmentCaseName(parts ...writtenFragment) string {
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		names = append(names, part.name)
	}
	return strings.Join(names, "/")
}

func keyEntryWritten(key, separator, value writtenFragment, content string) string {
	return key.prefix + ":" + separator.prefix + writtenAround(value, content)
}

func TestEverySeparatorSpellingIsClassifiedAsAnUnreadableKey(t *testing.T) {
	for _, key := range unreadableKeySpellings {
		for _, separator := range colonSeparatorSpellings {
			for _, value := range valueQuotings {
				t.Run(fragmentCaseName(key, separator, value), func(t *testing.T) {
					text := keyEntryWritten(key, separator, value, hiddenBeneathAKey)
					if !beginsWithUnreadableKeyIndicator(text) {
						t.Errorf("beginsWithUnreadableKeyIndicator(%q) = false, want true", text)
					}
				})
			}
		}
	}
}

func TestTheFallbackBlanksAnUnreadableKeyInEverySeparatorSpelling(t *testing.T) {
	for _, layout := range keyEntryLayouts {
		for _, key := range unreadableKeySpellings {
			for _, separator := range colonSeparatorSpellings {
				for _, value := range valueQuotings {
					t.Run(fragmentCaseName(layout, key, separator, value), func(t *testing.T) {
						entry := keyEntryWritten(key, separator, value, hiddenBeneathAKey)
						document := anchorDefinitionLine + writtenAround(layout, entry)
						assertFallbackDocumentHides(t, document+"\nbroken: [\n", hiddenBeneathAKey)
					})
				}
			}
		}
	}
}

func TestAReadableKeyStaysVisibleInEverySeparatorSpelling(t *testing.T) {
	for _, layout := range keyEntryLayouts {
		for _, key := range readableKeySpellings {
			for _, separator := range colonSeparatorSpellings {
				for _, value := range valueQuotings {
					t.Run(fragmentCaseName(layout, key, separator, value), func(t *testing.T) {
						entry := keyEntryWritten(key, separator, value, publicBeneathAKey)
						document := anchorDefinitionLine + writtenAround(layout, entry)
						if rendered := renderEveryLineOf(document + "\nbroken: [\n"); !strings.Contains(rendered, publicBeneathAKey) {
							t.Errorf("fallback removed public value:\n%s", rendered)
						}
					})
				}
			}
		}
	}
}

func TestAColonInsideContentIsNotReadAsAKeySeparator(t *testing.T) {
	for _, tc := range colonContentControls {
		t.Run(tc.name, func(t *testing.T) {
			if beginsWithUnreadableKeyIndicator(tc.text) {
				t.Errorf("beginsWithUnreadableKeyIndicator(%q) = true, want false", tc.text)
			}
		})
	}
}

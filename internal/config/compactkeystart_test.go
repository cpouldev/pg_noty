package config

import (
	"fmt"
	"strings"
	"testing"
)

var valueStartsAfterAnUnreadableKeysColon = []struct {
	name  string
	value string
}{
	{name: "empty at end of line"},
	{name: "space", value: " " + hiddenBeneathAKey},
	{name: "tab", value: "\t" + hiddenBeneathAKey},
	{name: "letter", value: hiddenBeneathAKey},
	{name: "digit", value: "7" + hiddenBeneathAKey},
	{name: "double quote", value: `"` + hiddenBeneathAKey + `"`},
	{name: "single quote", value: `'` + hiddenBeneathAKey + `'`},
	{name: "flow sequence", value: "[" + hiddenBeneathAKey + "]"},
	{name: "flow mapping", value: "{value: " + hiddenBeneathAKey + "}"},
	{name: "alias indicator", value: "*" + hiddenBeneathAKey},
	{name: "tag indicator", value: "!plain " + hiddenBeneathAKey},
	{name: "anchor indicator", value: "&plain " + hiddenBeneathAKey},
	{name: "literal block", value: "|\n  " + hiddenBeneathAKey},
	{name: "folded block", value: ">\n  " + hiddenBeneathAKey},
	{name: "comment indicator", value: "#" + hiddenBeneathAKey},
	{name: "punctuation", value: ":" + hiddenBeneathAKey},
	{name: "carriage return", value: "\r" + hiddenBeneathAKey},
	{name: "line feed", value: "\n" + hiddenBeneathAKey},
}

func TestEveryByteCanFollowTheColonOfAnUnreadableKey(t *testing.T) {
	for _, key := range unreadableKeySpellings {
		for number := 0; number <= 255; number++ {
			name := fmt.Sprintf("%s/byte-%03d", key.name, number)
			t.Run(name, func(t *testing.T) {
				valueStart := string([]byte{byte(number)})
				if !beginsWithUnreadableKeyIndicator(key.prefix + ":" + valueStart) {
					t.Errorf("byte %d changed the unreadable-key classification of %s", number, key.name)
				}
			})
		}
	}
}

func TestEveryMultibyteRuneCanFollowTheColonOfAnUnreadableKey(t *testing.T) {
	for _, key := range unreadableKeySpellings {
		for _, valueStart := range multibyteRuneClasses {
			t.Run(key.name+"/"+valueStart.name, func(t *testing.T) {
				if !beginsWithUnreadableKeyIndicator(key.prefix + ":" + valueStart.text) {
					t.Errorf("%s changed the unreadable-key classification of %s", valueStart.name, key.name)
				}
			})
		}
	}
}

func TestAnUnreadableKeyWithNoValueIsStillUnreadable(t *testing.T) {
	for _, key := range unreadableKeySpellings {
		t.Run(key.name, func(t *testing.T) {
			if !beginsWithUnreadableKeyIndicator(key.prefix + ":") {
				t.Errorf("empty value changed the unreadable-key classification of %s", key.name)
			}
		})
	}
}

func TestEveryValueStartAfterAnUnreadableKeyIsBlankedInEveryFallbackLayout(t *testing.T) {
	key := unreadableKeySpellings[0]
	for _, layout := range keyEntryLayouts {
		for _, valueStart := range valueStartsAfterAnUnreadableKeysColon {
			t.Run(layout.name+"/"+valueStart.name, func(t *testing.T) {
				entry := key.prefix + ":" + valueStart.value
				document := anchorDefinitionLine + writtenAround(layout, entry)
				rendered := renderEveryLineOf(document + "\nbroken: [\n")
				forbidden := hiddenBeneathAKey
				if valueStart.value == "" {
					forbidden = entry
				}
				if strings.Contains(rendered, forbidden) {
					t.Errorf("the fallback retained %s in %s", valueStart.name, layout.name)
				}
			})
		}
	}
}

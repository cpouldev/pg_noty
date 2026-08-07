package config

import "strings"

const unreadableKeyIndicators = "*&!?"

type flowKeyContext struct {
	kind       byte
	expectsKey bool
	atKeyStart bool
}

// beginsWithUnreadableKeyIndicator recognises a property, alias, or explicit-key introducer
// followed by the mapping colon it governs. The colon must be outside nested flow containers.
func beginsWithUnreadableKeyIndicator(text string) bool {
	if text == "" || !strings.ContainsRune(unreadableKeyIndicators, rune(text[0])) {
		return false
	}

	var quote byte
	var plainURI, verbatimTag bool
	var sequences, mappings int
	for at := 1; at < len(text); at++ {
		char := text[at]
		if quote != 0 {
			quote, at = insideQuotedScalar(text, at, quote)
			continue
		}
		if verbatimTag {
			verbatimTag = char != '>'
			continue
		}
		if plainURI {
			if char != ' ' && char != '\t' {
				continue
			}
			plainURI = false
		}
		if beginsYAMLComment(text, at) {
			return false
		}
		switch char {
		case '\'', '"':
			quote = char
		case '<':
			verbatimTag = at == 1 && text[0] == '!'
		case '[':
			sequences++
		case ']':
			sequences--
		case '{':
			mappings++
		case '}':
			mappings--
		case ':':
			if sequences != 0 || mappings != 0 {
				// A colon inside a flow container separates that container's own key from its
				// value, so it does not terminate the key this scan is reading.
				continue
			}
			if opensPlainURI(text, at) {
				plainURI = true
				continue
			}
			return true
		}
	}
	return false
}

func opensPlainURI(text string, colon int) bool {
	if !strings.HasPrefix(text[colon:], "://") {
		return false
	}
	before := text[:colon]
	boundary := strings.LastIndexAny(before, " \t")
	return boundary >= 0 && boundary+1 < colon
}

// opensAnUnreadableKey reports whether text, standing at a flow mapping's key boundary, begins a key
// whose name only a parser can decode: a property or alias introducer, a quoted name this file
// cannot read, or a nested container standing in for a name.
func opensAnUnreadableKey(text string) bool {
	return beginsWithUnreadableKeyIndicator(text) || beginsWithUnreadableQuotedKey(text) ||
		strings.HasPrefix(text, "[") || strings.HasPrefix(text, "{")
}

func beginsWithUnreadableQuotedKey(text string) bool {
	if text == "" || (text[0] != '\'' && text[0] != '"') {
		return false
	}
	return quotedLeadingToken(text) == aKeyThisFileCannotRead
}

// containsUnreadableFlowKey tracks whether a flow mapping is expecting a key. An alias in a
// value position is ordinary public source text; the same token at a key boundary is unreadable.
func containsUnreadableFlowKey(line string) bool {
	contexts := make([]flowKeyContext, 0, 4)
	var quote byte

	for at := 0; at < len(line); at++ {
		char := line[at]
		if quote != 0 {
			quote, at = insideQuotedScalar(line, at, quote)
			continue
		}
		if beginsYAMLComment(line, at) {
			return false
		}

		top := len(contexts) - 1
		if top >= 0 && contexts[top].kind == '{' && contexts[top].expectsKey {
			if char == ' ' || char == '\t' {
				continue
			}
			if contexts[top].atKeyStart && opensAnUnreadableKey(line[at:]) {
				return true
			}
			// Past the key's first rune, whatever it turned out to be.
			contexts[top].atKeyStart = false
		}

		switch char {
		case '\'', '"':
			quote = char
		case '{':
			contexts = append(contexts, flowKeyContext{'{', true, true})
		case '[':
			contexts = append(contexts, flowKeyContext{kind: '['})
		case '}', ']':
			if len(contexts) != 0 {
				contexts = contexts[:len(contexts)-1]
			}
		case ':':
			if top >= 0 && contexts[top].kind == '{' && contexts[top].expectsKey {
				contexts[top].expectsKey = false
			}
		case ',':
			if top >= 0 && contexts[top].kind == '{' {
				contexts[top].expectsKey = true
				contexts[top].atKeyStart = true
			}
		}
	}
	return false
}

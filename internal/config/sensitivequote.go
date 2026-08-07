package config

// sensitiveContinuation is fallback-only lexical state for a sensitive value that continues
// onto later source lines. It tracks both quote grammars and both flow containers because a
// key-shaped line at the key's own indentation is still value content while any is open.
type sensitiveContinuation struct {
	quote        byte
	sequences    int
	mappings     int
	taintedClose bool
}

var noSensitiveContinuation sensitiveContinuation

func sensitiveContinuationAfter(line string, at byteOffset) sensitiveContinuation {
	return noSensitiveContinuation.afterFrom(line, int(at))
}

func (s sensitiveContinuation) open() bool {
	return s.quote != 0 || s.sequences > 0 || s.mappings > 0 || s.taintedClose
}

// after consumes one complete line. YAML single quotes escape themselves by doubling; double
// quotes use a backslash escape. Flow depths are counted so nested malformed containers fail
// closed too.
func (s sensitiveContinuation) after(line string) sensitiveContinuation {
	return s.afterFrom(line, 0)
}

func (s sensitiveContinuation) afterFrom(line string, start int) sensitiveContinuation {
	if s.taintedClose {
		s.taintedClose = false
		return s
	}

	flowWasOpen := s.sequences > 0 || s.mappings > 0
	for i := start; i < len(line); i++ {
		char := line[i]
		if s.quote != 0 {
			s.quote, i = insideQuotedScalar(line, i, s.quote)
			continue
		}

		// A YAML comment ends the lexical line. Delimiters inside it cannot alter flow
		// state already carried to the next physical line.
		if beginsYAMLComment(line, i) {
			return s
		}

		switch char {
		case '\'', '"':
			s.quote = char
		case '[':
			s.sequences++
		case ']':
			if s.sequences > 0 {
				s.sequences--
			}
		case '{':
			s.mappings++
		case '}':
			if s.mappings > 0 {
				s.mappings--
			}
		}
		if flowWasOpen && s.sequences == 0 && s.mappings == 0 {
			s.taintedClose = !trustedFlowCloseSuffix(line[i+1:])
			return s
		}
	}
	return s
}

func trustedFlowCloseSuffix(suffix string) bool {
	separated := false
	for at := 0; at < len(suffix); at++ {
		switch suffix[at] {
		case ' ', '\t':
			separated = true
		case '\r', '\n':
			return true
		case '#':
			return separated
		default:
			return false
		}
	}
	return true
}

// insideQuotedScalar advances one byte of an open quoted scalar: it answers the quote still open
// after that byte -- zero once the scalar closed -- and the offset the scan resumes from.
//
// It is one function because YAML's two quote grammars differ only in how an escape is spelled, and
// every scan in this package that must not mistake a delimiter inside a quoted scalar for a real one
// needs both: a double-quoted scalar escapes with a backslash, a single-quoted one by doubling its
// quote, and either way the escaped byte is consumed here rather than re-examined.
func insideQuotedScalar(text string, at int, quote byte) (byte, int) {
	switch {
	case quote == '"' && text[at] == '\\':
		return quote, at + 1
	case text[at] != quote:
		return quote, at
	case quote == '\'' && at+1 < len(text) && text[at+1] == '\'':
		return quote, at + 1
	default:
		return 0, at
	}
}

func beginsYAMLComment(line string, at int) bool {
	if line[at] != '#' {
		return false
	}
	return at == 0 || line[at-1] == ' ' || line[at-1] == '\t'
}

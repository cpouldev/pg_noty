package config

// This file holds the lexical state D3's fallback branch carries from one line to the next, and
// the one transition that is not simply an arm's own answer.
//
// It is a file of its own because the state is what the walk in sensitivekeys.go is *about*, and
// keeping it beside the switch that reads it made that file the tightest in the subsystem. What
// the walk decides per line lives there; what a line hands to the line after it lives here.

// fallbackLineState is what one line of the walk hands to the line after it: the indentation of the
// sensitive block still open, and the quote or flow container still unclosed.
//
// The two travel as one value because they are one answer. They were two variables, and only the
// continuation was maintained in every arm -- blockIndent was answered where a block is opened and
// left to fall out of the switch in the other four. The rule that names that shape was extracted
// from this very switch and applied to one of its two carried states. Returning the state whole
// makes the omission unwriteable rather than commented on: an arm answering one half does not
// compile. Stating it the other way would mean assigning blockIndent to itself, which go vet
// refuses.
type fallbackLineState struct {
	blockIndent  int
	continuation sensitiveContinuation
}

// blanked is the state a line that was blanked whole hands on: the block stays open at the
// indentation it was opened at, because blanking a line closes nothing and the three arms that
// blank are reached precisely because something already covers the line; and the continuation is
// advanced from the line's own text, because a quote or a flow container can be opened on any line
// of a sensitive block and not only on its key's.
func (state fallbackLineState) blanked(line string) fallbackLineState {
	return fallbackLineState{
		blockIndent:  state.blockIndent,
		continuation: state.continuation.after(line),
	}
}

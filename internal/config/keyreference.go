package config

import (
	"strings"

	"github.com/goccy/go-yaml/ast"
)

// This file is AC #6 -- interpolation reaches values and never keys -- as one decision with one
// home. Two stages have to ask it: stage D of every key an author wrote, and stage E of every key
// the operations fold invents out of a list element stage D had already substituted into. They
// ask it here rather than each encoding it, because a security-relevant condition with two
// implementations is one implementation plus something that used to be true. That the two stages
// answer alike is asserted by TestBothStagesThatMeetAKeyGiveItOneVerdict, which drives both
// stages rather than this function, so a condition added to one and not the other fails by name.
//
// It answers rather than reports, because what the stages do with the answer differs: stage D
// records it and walks on, stage E abandons the fold the key was being invented for. Each keeps
// that decision at its own call site.

// What this decision says about the two conditions that are about *where* a reference was written
// rather than about how it was written. Neither quotes the document's own text, for the reason
// envreference.go's refusals record: a message is rendered exactly as it is built, so a message
// carrying a key's text would be one path by which a secret reaches output un-redacted (ADR-5).
const (
	keyReferenceMessage = "interpolation is not applied to mapping keys"
	keyReferenceHint    = "write the key out in full, and put the ${...} reference in its value"

	unrecognisedKeyShapeMessage = "unsupported YAML shape in a key position"
	unrecognisedKeyShapeHint    = "a key has to be readable before it can be checked for ${...}; write it as a scalar"
)

// The two refusals, as the faults both stages raise. A reader must not be able to tell which
// stage found a key holding `${`, since it is one condition and one remedy.
var (
	referenceInKey = fault{message: keyReferenceMessage, hint: keyReferenceHint}
	unreadableKey  = fault{message: unrecognisedKeyShapeMessage, hint: unrecognisedKeyShapeHint}
)

// keyReferenceFault is why this key may not stand, and whether there is a reason at all.
//
// Any occurrence of the opening delimiter refuses the key, the escape included. A key is never
// interpolated, so `$${` is not unescaped inside one either, and a key written that way would
// silently keep both of its dollars.
//
// A key whose shape cannot be read is refused rather than cleared, and unconditionally: a key
// this package cannot read is a key it cannot clear of holding a reference, and asking the node
// for its own rendering instead would be a guard the input can switch off. One shape a document
// can write reaches that arm -- an alias in a key position, which keyTextOf declines because
// stage E would replace it with a value stage D has already substituted the environment into.
func keyReferenceFault(key ast.Node) (fault, bool) {
	text, readable := keyTextOf(key)
	if !readable {
		return unreadableKey, true
	}
	if strings.Contains(text, referenceOpen) {
		return referenceInKey, true
	}
	return fault{}, false
}

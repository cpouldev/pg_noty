package source

// This is the closed payload vocabulary internal/config accepts. A new mode must join config's
// payloadModes declaration and this value; the AST reconciliation in payloadmode_test.go keeps
// the substitute from silently falling through to an empty expression.
var payloadModes = [...]string{"full", "columns", "keys_only"}

type unknownPayloadModeError struct{ mode string }

func (e unknownPayloadModeError) Error() string {
	return "unknown payload mode " + e.mode + ": no payload expression"
}

func payloadModeKnown(mode string) error {
	for _, known := range payloadModes {
		if known == mode {
			return nil
		}
	}
	return unknownPayloadModeError{mode: mode}
}

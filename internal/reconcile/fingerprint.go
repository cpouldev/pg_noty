package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
)

// The generator-text design described by this column's name and its internal/schema comment was
// rejected, and neither can be edited: runnerledger.go:129-132 rejects changed migrations for
// already-bootstrapped databases. A fingerprint therefore identifies catalog readbacks, with their
// composition fixed here and compared only by the later drift owner.
const fingerprintVersion = "1:"

// fingerprint returns the versioned fingerprint for complete trigger and function definitions.
// The NUL separator has its own justification: PostgreSQL text cannot carry a NUL, unlike the
// lock-key separator, which belongs to a separate domain and derivation.
func fingerprint(triggerDefinition, functionDefinition string) string {
	sum := sha256.Sum256([]byte(triggerDefinition + "\x00" + functionDefinition))
	return fingerprintVersion + hex.EncodeToString(sum[:])
}

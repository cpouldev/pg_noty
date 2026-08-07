package schema

import (
	"fmt"
	"hash/fnv"
	"unicode/utf8"
)

// This L1 file owns the deterministic object-name derivation shared with the trigger compiler.
// It is separate from partition.go because that file is already near its artifact budget; both
// packages read this authority rather than maintaining a second truncate-plus-hash body.

const (
	// MaxIdentifierBytes is PostgreSQL's NAMEDATALEN-1 identifier limit.
	MaxIdentifierBytes = 63
	// identifierHashHexDigits is the width of the deterministic tag ObjectName appends when it has
	// to truncate.
	identifierHashHexDigits = 8
)

// ObjectName joins a base and a suffix into an identifier PostgreSQL will not truncate. This is
// skill Pattern 9's truncate-plus-deterministic-hash shape, and the hash is taken over the whole
// pre-truncation name for the reason that pattern exists: truncating alone is what collides two
// names agreeing on their first 63 bytes, so a tag derived from the truncated form would collide
// with them too (M9).
func ObjectName(base, suffix string) string {
	full := base + "_" + suffix
	if len(full) <= MaxIdentifierBytes {
		return full
	}

	digest := fnv.New32a()
	_, _ = digest.Write([]byte(full))
	tag := fmt.Sprintf("%0*x", identifierHashHexDigits, digest.Sum32())

	return full[:runeBoundaryAtOrBefore(full, MaxIdentifierBytes-len(tag)-1)] + "_" + tag
}

// runeBoundaryAtOrBefore is the largest cut index no greater than limit that does not fall inside a
// UTF-8 sequence. Every name this package generates is ASCII, so the walk is a guard rather than a
// live path; it is reached directly by TestAnOverLongNameIsCutOnARuneBoundary.
func runeBoundaryAtOrBefore(text string, limit int) int {
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return limit
}

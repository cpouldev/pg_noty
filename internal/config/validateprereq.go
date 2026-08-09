package config

import (
	"slices"
	"strings"
)

const (
	minConcurrency = 1
	maxConcurrency = 1024
)

// concurrencyCanParticipateInComparison is the D1 boundary between Step 8's own-value ranges and
// Step 11's effective R39 comparison. Stage I must require this predicate of both endpoints before
// comparing them; otherwise one invalid endpoint produces a range diagnostic and a second,
// derivative comparison diagnostic.
func concurrencyCanParticipateInComparison(value int) bool {
	return value >= minConcurrency && value <= maxConcurrency
}

// payloadModeCanGovernCompatibility is both R31's accepted class and Step 9's prerequisite for the
// mode-compatibility clauses of R32/R33. An omitted mode has not failed R31 and may use its default;
// a written mode can govern only when its wrapper read it and it names a permitted mode.
func payloadModeCanGovernCompatibility(mode Str) bool {
	return !mode.Set || mode.Valid() && slices.Contains(payloadModes, mode.value)
}

// hasPriorCorrection suppresses a derived "missing" complaint when stage F already identified the
// written key that was meant to satisfy it. The unknown-key diagnostic remains primary, while
// unrelated stage-H mistakes continue accumulating.
func (v *validatePass) hasPriorCorrection(at Positioned, intended string) bool {
	path := at.Path()
	dot := strings.LastIndexByte(path, '.')
	if dot < 0 {
		return false
	}
	prefix := path[:dot+1]
	for _, diag := range v.prior {
		if diag.Rule == R41 && strings.HasPrefix(diag.Path, prefix) &&
			diag.Hint == didYouMean(intended) {
			return true
		}
	}
	return false
}

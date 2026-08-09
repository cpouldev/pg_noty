package config

import (
	"slices"
	"strings"
)

// splitHalfOf classifies only diagnostics that were actually emitted. Fixture
// names and manifests cannot manufacture half coverage.
func splitHalfOf(diag Error) ruleHalf {
	switch diag.Rule {
	case R1:
		return presenceOrValueHalf(diag, "R1")
	case R3:
		return presenceOrValueHalf(diag, "R3")
	case R23:
		return presenceOrValueHalf(diag, "R23")
	case R26:
		return presenceOrValueHalf(diag, "R26")
	case R29:
		if strings.Contains(diag.Msg, "legal only under") {
			return r29LegalityHalf
		}
		return r29ContentHalf
	case R36:
		return presenceOrValueHalf(diag, "R36")
	case R39:
		if strings.Contains(diag.Msg, "worker.concurrency") {
			return r39CompareHalf
		}
		return r39RangeHalf
	default:
		return ""
	}
}

func presenceOrValueHalf(diag Error, rule string) ruleHalf {
	kind := "value"
	if strings.HasPrefix(diag.Msg, "missing required key") {
		kind = "presence"
	}
	return ruleHalf(rule + ":" + kind)
}

func compactHalves(halves []ruleHalf) []ruleHalf {
	halves = append([]ruleHalf(nil), halves...)
	slices.Sort(halves)
	return slices.Compact(halves)
}

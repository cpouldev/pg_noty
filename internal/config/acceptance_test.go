package config

import (
	"slices"
	"strings"

	"github.com/goccy/go-yaml/ast"
)

type acceptanceClaim struct {
	rule RuleID
	half ruleHalf
}

type acceptanceSubject struct {
	id      string
	source  string
	matches func(ast.Node) bool
	mutate  acceptanceMutation
}

type acceptanceObservation struct {
	subject     string
	state       string
	needsConfig bool
	holds       func(acceptanceEvidence) bool
}

type acceptanceSpec struct {
	claim        acceptanceClaim
	fixture      string
	subjects     []acceptanceSubject
	observations []acceptanceObservation
}

type acceptanceEvidence struct {
	root   ast.Node
	config *Config
}

type acceptanceMutation func([]byte) ([]byte, error)

func wholeAcceptance(rule RuleID, fixture string, subjects []acceptanceSubject,
	observations []acceptanceObservation,
) acceptanceSpec {
	return acceptanceSpec{acceptanceClaim{rule: rule}, fixture, subjects, observations}
}

func halfAcceptance(rule RuleID, half ruleHalf, fixture string, subjects []acceptanceSubject,
	observations []acceptanceObservation,
) acceptanceSpec {
	return acceptanceSpec{acceptanceClaim{rule: rule, half: half}, fixture, subjects, observations}
}

func acceptanceSpecs() []acceptanceSpec {
	return slices.Concat(
		acceptanceSpecsR1ToR10(),
		acceptanceSpecsR11ToR21(),
		acceptanceSpecsR22ToR29(),
		acceptanceSpecR30(),
		acceptanceSpecsR31ToR38(),
		acceptanceSpecsR39ToR42(),
		acceptanceWarningSpecs(),
	)
}

func acceptanceUniverse() []acceptanceClaim {
	var claims []acceptanceClaim
	for _, rule := range slices.Concat(staticRuleIDs, warningRuleIDs) {
		halves := splitRuleHalves[rule]
		if len(halves) == 0 {
			claims = append(claims, acceptanceClaim{rule: rule})
			continue
		}
		for _, half := range halves {
			claims = append(claims, acceptanceClaim{rule: rule, half: half})
		}
	}
	sortAcceptanceClaims(claims)
	return claims
}

func sortAcceptanceClaims(claims []acceptanceClaim) {
	slices.SortFunc(claims, func(a, b acceptanceClaim) int {
		if byRule := strings.Compare(string(a.rule), string(b.rule)); byRule != 0 {
			return byRule
		}
		return strings.Compare(string(a.half), string(b.half))
	})
}

func (claim acceptanceClaim) String() string {
	if claim.half != "" {
		return string(claim.half)
	}
	return string(claim.rule)
}

package config

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// This file is how a scalar becomes a Go value: the one text every wrapper starts from, and the
// conversion each of them applies to it. The wrappers are scalar.go's; what they have in common is
// here, because four conversions reading four different texts would be four chances to disagree.
//
// **One text, not one decode per type, and that is a measured requirement rather than a tidiness
// preference.** The obvious shape -- `yaml.NodeToValue(node, &int)` in `Int`, `&bool` in `Bool` --
// answers differently for the same input depending on the target: measured on v1.19.2, a string node
// holding "16" decodes into an int, and a string node holding "true" does **not** decode into a bool
// (`cannot unmarshal string into Go value of type bool`). After stage D every interpolated value *is*
// a string node, whatever it now holds, so that shape would make AC #5 hold for `worker.concurrency:
// ${N}` and silently fail for `retry.jitter: ${J}` -- the same guarantee true of one field type and
// false of another, which is the failure mode ADR-2 exists to remove.
// TestGoccyReadsAnInterpolatedIntegerButNotAnInterpolatedBoolean pins both halves of that
// measurement, so a library that closes the gap is noticed here rather than through a wrapper that
// has become needlessly indirect.
//
// Reading the text through the library rather than through this package's own scalarTextOf is
// deliberate too: the library answers for every scalar kind, and answers with the *parsed* value
// rather than the written one -- `0x10` reads as `16` and `True` as `true` -- which is the same
// collapsing of spellings that keyIdentity performs on the key side (nontextualscalar.go). A wrapper
// reading the source text would answer 0 for `0x10`.

// valueTextOf is the text a scalar holds, whatever kind the parser read it as, and whether the node
// is a scalar at all.
//
// The node properties an author may write on a value are read off first, because a tag is a claim
// about the value rather than a value of its own and stage E leaves it standing. Without the peel
// every tagged scalar -- `version: !!int 1`, which
// valid/shape_ok_values_carrying_node_properties.yaml writes -- would be refused as a shape no
// wrapper can read.
//
// A mapping and a list are the two shapes it answers false for, which is what every wrapper's
// "expected a scalar" refusal rests on.
//
// **The peel is currently redundant, and is kept deliberately.** Mutation testing found it: removing it
// changes no answer, because the library peels a tag itself when the target is a scalar -- while refusing
// one when the target is a struct, which is the asymmetry rawcontainer.go exists for.
// TestGoccyReadsAScalarThroughItsOwnTag pins the half that makes this line redundant, so an upgrade that
// stops peeling turns it load-bearing and says so by name rather than silently. It stays because the
// alternative is a file where one wrapper reads past a tag and StrList's own assertion does not, and a
// reader would need to know the library's asymmetry to see why.
func valueTextOf(node ast.Node) (string, bool) {
	var text string
	if err := yaml.NodeToValue(beneathNodeProperties(node), &text); err != nil {
		return "", false
	}
	return text, true
}

// The two boolean spellings the YAML core schema gives, folded to one case.
//
// Exactly these two, rather than strconv.ParseBool's wider set: ParseBool also accepts `1`, `0`, `t`
// and `f`, and `jitter: 1` is an integer in YAML rather than a flag -- accepting it would make R18's
// "jitter is a boolean" true of a value YAML says is not one. Folding case is what keeps the literal
// and the interpolated spelling one answer: the parser normalises `True` to `true` before a wrapper
// sees it, and after stage D `${J}` holding `True` is text the parser never read.
const (
	writtenTrue  = "true"
	writtenFalse = "false"
)

// asBool is the boolean a scalar's text states, and whether it states one.
func asBool(text string) (bool, bool) {
	switch strings.ToLower(text) {
	case writtenTrue:
		return true, true
	case writtenFalse:
		return false, true
	}
	return false, false
}

// asInt is the integer a scalar's text states, and whether it states one. A value outside the range
// of an int is not one, which is strconv's answer rather than a check of this package's.
func asInt(text string) (int, bool) {
	value, err := strconv.Atoi(text)
	return value, err == nil
}

// durationUnitFamilies is the single production spelling of R40's accepted units. The parser,
// reference and refusal all derive from it; their tests independently pin the contract vocabulary.
const (
	durationUnitFamilies  = "ns, us/µs, ms, s, m, h"
	durationRefusalPrefix = "must use a Go duration with units "
)

var durationUnitSpellings = strings.FieldsFunc(durationUnitFamilies, func(char rune) bool {
	return char == ',' || char == '/' || char == ' '
})

var durationRefusal = durationRefusalPrefix + durationUnitsForSentence()

func durationUnitsForSentence() string {
	last := strings.LastIndex(durationUnitFamilies, ", ")
	return durationUnitFamilies[:last] + " or " + durationUnitFamilies[last+2:]
}

// durationUsesOnlyRatifiedUnits checks only unit tokens. time.ParseDuration remains the numeric
// grammar, while this preflight prevents an implementation synonym or future library unit from
// silently widening the configuration contract.
func durationUsesOnlyRatifiedUnits(text string) bool {
	units := strings.FieldsFunc(text, func(char rune) bool {
		return char == '+' || char == '-' || char == '.' || char >= '0' && char <= '9'
	})
	for _, unit := range units {
		if !slices.Contains(durationUnitSpellings, unit) {
			return false
		}
	}
	return true
}

// asDuration is the duration a scalar's text states, and whether it states one.
//
// This is the single parser boundary the carried-forward `d`/`w` question widens. Go also accepts
// Greek U+03BC `μs`; the ratified vocabulary contains ASCII `us` and micro sign U+00B5 `µs`, so the
// unit preflight refuses the Greek spelling before delegating.
func asDuration(text string) (time.Duration, bool) {
	if !durationUsesOnlyRatifiedUnits(text) {
		return 0, false
	}
	value, err := time.ParseDuration(text)
	return value, err == nil
}

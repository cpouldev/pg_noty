package config

import (
	"time"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// This file is ADR-2's keystone: one wrapper per scalar kind the contract declares, each of which
// records what it read, records what it could not read, and **returns nil either way**.
//
// CONFORMANCE with skill Pattern 6 (the keystone scalar NodeUnmarshaler). The mechanism is the
// pattern's own: implement yaml.NodeUnmarshaler on a small wrapper type per scalar kind, convert
// inside it, record a positioned diagnostic on failure, and never return a real error. Pattern 6's
// flags are called `Present` and `Valid`; this package calls the first one `Set` and keeps the second,
// and presence.go records that mapping at the type as well as here so a reader arriving from the
// skill finds it wherever they enter the code.
//
// **Why the return value is the whole design.** One yaml.NodeToValue call decodes the whole document.
// If any wrapper answered non-nil, that call would abandon the rest of the document, and every
// acceptance criterion that counts diagnostics in one run -- ten unknown keys, five mistakes, three
// later mistakes after an interpolation is fixed -- would become unreachable for reasons that look
// like flakiness rather than like a design decision. So the guarantee is not "no wrapper happens to
// fail today": no UnmarshalYAML below has any exit that returns anything but nil -- several have more
// than one such exit, one per refusal, and what matters is that none of them can answer otherwise. That
// is asserted per wrapper by TestEveryWrapperReturnsNilForAnInvalidValueOfItsOwnType, over an unbounded
// input domain by the five fuzz targets in scalarfuzz_test.go, and structurally -- over the source
// rather than over any input -- by TestNoWrapperHasAnExitThatCanReturnAnError.
//
// The library honouring it is a dependency rather than an assumption:
// TestGoccyLetsANilReturningScalarUnmarshalerMakeDecodeUnableToFail is V8's pin.
//
// None of the refusals below quotes the value it refused, for the reason every other stage's do not:
// a message is rendered exactly as it is built, so a message carrying a value's text would be a path
// by which an interpolated secret reaches output un-redacted (ADR-5). What each one names is the type
// the contract declares, which is what AC #5 asks a type diagnostic to say.

// The compile-time half of the two claims this file makes. A wrapper that stopped implementing
// yaml.NodeUnmarshaler would silently decode as a plain struct -- the decoder would fill no field of
// it, and every value would read as absent -- and a wrapper that stopped satisfying Positioned would
// push an ast import into the rule layer. Neither is a failure a test would name, so both are stated
// as assertions the build has to satisfy.
var (
	_ yaml.NodeUnmarshaler = (*Str)(nil)
	_ yaml.NodeUnmarshaler = (*Int)(nil)
	_ yaml.NodeUnmarshaler = (*Bool)(nil)
	_ yaml.NodeUnmarshaler = (*Dur)(nil)

	_ Positioned = Str{}
	_ Positioned = Int{}
	_ Positioned = Bool{}
	_ Positioned = Dur{}
)

// What each wrapper declines to read, as the fault it raises, declared beside the conversion that
// raises it.
//
// mustBeADuration is the one whose final wording belongs elsewhere: R40's diagnostic has to list the
// accepted Go units and suggest the hour equivalent of what the author wrote, and **Step 8 owns that
// wording**. It is stated plainly here so the capture is positioned and countable meanwhile.
var (
	mustBeText      = fault{message: "expected text"}
	mustBeAnInteger = fault{message: "expected an integer"}
	mustBeABoolean  = fault{message: "expected true or false"}
	mustBeADuration = fault{message: durationRefusal}
)

// Str is a text value: every key the contract declares as a name, a URL, an identifier or an enum.
type Str struct {
	presence
	value string
}

// UnmarshalYAML reads the text this node holds.
//
// The refusal is reachable by no document, because stage F refuses a mapping or a list where the
// contract declares a scalar and stops the run before decode. It is reached directly by
// TestEveryWrapperReturnsNilForAnInvalidValueOfItsOwnType, because a fail-closed branch nothing
// asserts is a branch that can be deleted with the suite still green.
func (s *Str) UnmarshalYAML(node ast.Node) error {
	s.began(node)

	text, isScalar := valueTextOf(node)
	if !isScalar {
		s.refuse(mustBeText)
		return nil
	}
	s.value = text
	return nil
}

// Int is a counted value: a version, a pool size, a batch size, a byte cap, an attempt count.
type Int struct {
	presence
	value int
}

func (i *Int) UnmarshalYAML(node ast.Node) error {
	i.began(node)

	text, isScalar := valueTextOf(node)
	if !isScalar {
		i.refuse(mustBeAnInteger)
		return nil
	}
	value, isInteger := asInt(text)
	if !isInteger {
		i.refuse(mustBeAnInteger)
		return nil
	}
	i.value = value
	return nil
}

// Bool is a flag: `enabled`, `jitter`, `include_old`.
type Bool struct {
	presence
	value bool
}

func (b *Bool) UnmarshalYAML(node ast.Node) error {
	b.began(node)

	text, isScalar := valueTextOf(node)
	if !isScalar {
		b.refuse(mustBeABoolean)
		return nil
	}
	value, isBoolean := asBool(text)
	if !isBoolean {
		b.refuse(mustBeABoolean)
		return nil
	}
	b.value = value
	return nil
}

// resolve assigns the three schema-declared boolean conversions to their numbered rules. A Bool
// used outside a declared key retains stage G's structural rule, as every direct wrapper test does.
func (b *Bool) resolve(pass *decodePass) {
	reportedBy := noRule
	if b.node != nil {
		reportedBy = conversionRuleAt(pathOf(b.node))
	}
	if reportedBy == noRule {
		reportedBy = pass.rule
	}
	if reportedBy == pass.rule {
		b.resolveAs(pass, reportedBy)
		return
	}
	b.resolveSemanticAs(pass, reportedBy)
}

// Dur is a duration: every interval, timeout and retention span.
//
// It is the one wrapper whose refusal the contract states as a numbered rule. R40 is written over
// "every duration value" rather than over a key, which is exactly the scope a wrapper has, so a
// duration this package cannot read is reported as R40 wherever it is written -- and the rules that
// judge a duration's *magnitude* (R8, R10, R13, R17) fire only on one it could read.
type Dur struct {
	presence
	value time.Duration
}

func (d *Dur) UnmarshalYAML(node ast.Node) error {
	d.began(node)

	text, isScalar := valueTextOf(node)
	if !isScalar {
		d.refuse(mustBeADuration)
		return nil
	}
	value, isDuration := asDuration(text)
	if !isDuration {
		why := mustBeADuration
		if text == durationDayExample {
			why.hint = durationDayHint
		}
		d.refuse(why)
		return nil
	}
	d.value = value
	return nil
}

// resolve reports a duration this wrapper could not read under R40 rather than under the stage's own
// structural rule. It is the one exception to "the stage names the rule", and it is an exception because
// the contract states this one over every duration value -- which is exactly a wrapper's scope.
func (d *Dur) resolve(pass *decodePass) {
	d.resolveSemanticAs(pass, R40)
}

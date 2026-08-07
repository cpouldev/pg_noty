package config

import (
	"errors"
	"regexp"
	"slices"
	"testing"

	"github.com/goccy/go-yaml"
)

// **V4**, both findings, and this is the only file of the package permitted to name a strictness option: it
// exists to measure the finding that justifies excluding one everywhere else
// (TestNoStrictnessOptionIsPassedAnywhere scopes the exemption to the V4 pins by name).
//
// **V4's first finding is two facts, and only one of them is stable. The first version of this file
// conflated them.** It asserted that strict decode reports the *first* unknown field -- an ordering the
// library nowhere promises -- and that assertion failed about one run in eight, caught by `-count=1` over
// the whole module rather than by any single green run.
//
// Measured on v1.19.2, 2000 strict decodes in **one process** against a document holding three unknown
// fields: every call named exactly **one** of them (2000/2000), and which one varied per call -- 1501
// times `b`, 260 times `d`, 239 times `c`. The choice is therefore nondeterministic *within* a process and
// not merely across processes, and the position the message embeds moves with it.
//
// The count is the fact ADR-1 rests on, and it is pinned below by equality rather than by a bound. The
// identity is pinned as the instability it is, which is strictly more than the old assertion pretended to
// know: strict decode could not have produced user-facing diagnostics even if it reported positions well,
// because two runs over one unchanged file would blame different keys. That makes the case for the
// hand-rolled unknown-key walk stronger rather than weaker.

// threeUnknownFields holds one declared key and three the struct does not have. Three rather than two so
// that "fewer than the document holds" has room in it: a version reporting two of three fails the equality
// below exactly as one reporting all three does.
const threeUnknownFields = "a: 1\nb: 2\nc: 3\nd: 4\n"

// theUnknownFields is what a report may legally name, so one naming something else -- a declared key, or
// nothing recognisable -- fails rather than counting as "one field".
var theUnknownFields = []string{"b", "c", "d"}

// unknownFieldNamed matches the field name inside the library's own wording, which is what lets the two
// facts be asked separately: how many fields a report names, and which.
var unknownFieldNamed = regexp.MustCompile(`unknown field "([^"]+)"`)

// unknownFieldsNamed is every field name a strict-decode error names.
//
// Names are extracted rather than occurrences of the phrase counted, because the case the equality below
// exists to catch is a version that names all three -- and that has to be counted as three.
func unknownFieldsNamed(err error) []string {
	if err == nil {
		return nil
	}

	var named []string
	for _, match := range unknownFieldNamed.FindAllStringSubmatch(err.Error(), -1) {
		named = append(named, match[1])
	}
	return named
}

// TestGoccyStrictDecodeSurfacesOneErrorPerCall is **V4**'s stable half, and the one that makes AC #8
// unreachable through the library: three unknown fields yield one error naming one of them.
//
// It is an equality and not a bound. A version that enumerated every unknown field would make strict
// decode a candidate for the unknown-key walk again, and that has to fail here rather than slip past a
// "reports at least one" assertion.
//
// The claim is asserted over many calls rather than one because the library's choice varies per call: one
// call cannot tell "reports one" from "reported one this time".
func TestGoccyStrictDecodeSurfacesOneErrorPerCall(t *testing.T) {
	var into struct {
		A int `yaml:"a"`
	}

	for range 50 {
		err := yaml.NodeToValue(parsedRoot(t, threeUnknownFields), &into, yaml.Strict())
		if err == nil {
			t.Fatal("strict decode accepted three unknown fields; V4's first finding no longer holds")
		}

		named := unknownFieldsNamed(err)
		if len(named) != 1 {
			t.Fatalf("strict decode named %d unknown fields %v (%v); V4 measured exactly one per call, and "+
				"the hand-rolled unknown-key walk exists because one is fewer than the document holds",
				len(named), named, err)
		}
		if !slices.Contains(theUnknownFields, named[0]) {
			t.Fatalf("strict decode named %q, which is none of the document's unknown fields %v",
				named[0], theUnknownFields)
		}
	}
}

// TestGoccyStrictDecodeDoesNotPromiseWhichUnknownFieldItReports records V4's unstable half as the measured
// fact it is, rather than leaving it to a comment -- and it is the assertion the old one should have been.
//
// A failure here most probably means the library has *stabilised* its choice, which deserves a named
// failure rather than silence: under the measured distribution the most frequent field wins about three
// calls in four, so one field winning all 1000 calls by chance is on the order of 1e-125. Either way it is
// no reason to reinstate strictness -- a deterministic choice still reports one field of three, so ADR-1's
// exclusion rests on the equality above and not on this.
func TestGoccyStrictDecodeDoesNotPromiseWhichUnknownFieldItReports(t *testing.T) {
	var into struct {
		A int `yaml:"a"`
	}

	seen := map[string]int{}
	for range 1000 {
		named := unknownFieldsNamed(yaml.NodeToValue(parsedRoot(t, threeUnknownFields), &into, yaml.Strict()))
		if len(named) == 1 {
			seen[named[0]]++
		}
	}

	if len(seen) < 2 {
		t.Errorf("all 1000 strict decodes named the same unknown field (%v).\n"+
			"This is most likely a BENIGN library change -- goccy stabilising which unknown field it "+
			"reports -- and NOT a defect in this package. Nothing needs fixing to make it pass: update this "+
			"file's opening measurement to record the new behaviour, and delete this test, because a stable "+
			"choice is no longer a fact worth pinning. Do NOT weaken "+
			"TestGoccyStrictDecodeSurfacesOneErrorPerCall, which is the pin ADR-1 actually rests on and is "+
			"unaffected either way.", seen)
	}
	t.Logf("which unknown field was named, over 1000 calls: %v", seen)
}

// TestTheOneErrorPerCallPinWouldSeeAVersionThatReportedThemAll is the equality's own falsifiability, which
// no run against the current library can demonstrate: it reports one field, so the assertion passes and
// says nothing about what it would do with three.
//
// Without this the pin could be satisfied by a reader that only ever found the first name -- a
// `FindStringSubmatch` in place of `FindAllStringSubmatch`, say -- and a library that started enumerating
// every unknown field would then be counted as one and pass. The whole accumulate design rests on that
// case being caught, so the counting is exercised against a message holding all three.
func TestTheOneErrorPerCallPinWouldSeeAVersionThatReportedThemAll(t *testing.T) {
	// The library's own wording, repeated once per field, which is the shape a version reporting the whole
	// set would produce.
	reportedAll := errors.New(`[2:1] unknown field "b"` + "\n" + `[3:1] unknown field "c"` + "\n" +
		`[4:1] unknown field "d"`)

	if named := unknownFieldsNamed(reportedAll); !slices.Equal(named, theUnknownFields) {
		t.Errorf("the reader found %v in a message naming all of %v; a version that enumerated every "+
			"unknown field would be counted as one and pass the equality above", named, theUnknownFields)
	}
	if named := unknownFieldsNamed(nil); named != nil {
		t.Errorf("the reader found %v in the absence of an error, so a passing decode would be counted "+
			"as a report", named)
	}
}

// TestGoccyStrictDecodeIsBlindInsideAnUnmarshaler is **V4**'s second finding, and the measurement
// decode.go's divergence comment rests on: strictness sees nothing inside a NodeUnmarshaler subtree, so
// offering it as defence in depth would claim cover over exactly the two levels -- operations and
// headers -- that this package reaches through one.
//
// Both halves are here, because "strict is blind inside a subtree" means nothing without a case where
// strict is not blind: the same unknown key one level up is reported. Neither half asserts *which* key, and
// neither document holds more than one unknown field, so neither is exposed to the instability above.
func TestGoccyStrictDecodeIsBlindInsideAnUnmarshaler(t *testing.T) {
	var into struct {
		Sub pinnedBlock `yaml:"sub"`
	}

	t.Run("inside the subtree", func(t *testing.T) {
		if err := yaml.NodeToValue(parsedRoot(t, "sub:\n  unknown_inner: 1\n"), &into, yaml.Strict()); err != nil {
			t.Fatalf("strict decode reported %v inside a NodeUnmarshaler subtree; V4's second finding no "+
				"longer holds, so decode.go's divergence needs re-deriving", err)
		}
		if !into.Sub.called {
			t.Error("the unmarshaler was not called, so the row asserts nothing about its subtree")
		}
	})

	t.Run("beside the subtree", func(t *testing.T) {
		if err := yaml.NodeToValue(parsedRoot(t, "sub: {}\nunknown_outer: 1\n"), &into, yaml.Strict()); err == nil {
			t.Fatal("strict decode accepted an unknown field outside every subtree, so the row above " +
				"would pass for a strictness option that does nothing at all")
		}
	})
}

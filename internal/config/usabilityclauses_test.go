package config

import (
	"slices"
	"strings"
	"testing"
)

// diagnosticUsabilityIssues accumulates four independent reasons to reject a diagnostic behind one
// slice. A case asserted only as "the gate rejected it" cannot say which clause did, so any clause
// whose inputs also violate another can be deleted with the suite green -- and both the locator
// requirement and the whole deny-list were in that position: every negative case also carried an
// empty Hint, so the condition clause fired for all of them.
//
// Every case below therefore satisfies the three clauses it is not named for, and states the exact
// set of reasons it must draw. Where a case cannot satisfy them all -- a denied phrase shorter than
// the length floor is two violations in one string -- it names both, so the row still fails if
// either clause is removed.

type usabilityClauseCase struct {
	name string
	diag Error
	want []string
}

// deniedPhraseCases pair each denied phrase with the exact reasons it draws. The rune counts are
// counted from the phrases themselves rather than recomputed by the gate's own predicate, so a row
// cannot inherit the mistake it is meant to catch.
var deniedPhraseCases = []usabilityClauseCase{
	// "invalid value" is 13 runes and "unexpected value" 16, both past the 12-rune floor, so the
	// deny-list is the only clause left to reject them.
	{name: "invalid value", diag: deniedPhraseDiagnostic("invalid value"),
		want: []string{deniedPhraseIssue}},
	{name: "unexpected value", diag: deniedPhraseDiagnostic("unexpected value"),
		want: []string{deniedPhraseIssue}},

	// "bad value" is 9 runes, "invalid" 7 and "error" 5: each is below the floor as well as denied,
	// and no phrasing can separate the two, so both reasons are named.
	{name: "bad value", diag: deniedPhraseDiagnostic("bad value"),
		want: []string{deniedPhraseIssue, shortMessageIssue}},
	{name: "invalid", diag: deniedPhraseDiagnostic("invalid"),
		want: []string{deniedPhraseIssue, shortMessageIssue}},
	{name: "error", diag: deniedPhraseDiagnostic("error"),
		want: []string{deniedPhraseIssue, shortMessageIssue}},
}

// deniedPhraseDiagnostic writes a diagnostic that satisfies the locator and condition clauses, so
// only the deny-list -- and the length floor, where the phrase is short -- can reject it.
func deniedPhraseDiagnostic(phrase string) Error {
	return Error{Path: "value", Msg: phrase, Hint: "name the value the field must hold"}
}

func TestEachUsabilityClauseRejectsACaseOnlyItRejects(t *testing.T) {
	cases := slices.Clone(deniedPhraseCases)
	cases = append(cases,
		// Path empty, and everything else satisfied: 30 runes, no denied phrase, and a Hint that
		// answers the condition clause. Only the locator requirement is left.
		usabilityClauseCase{
			name: "empty path with every other clause satisfied",
			diag: Error{Msg: "must equal the documented value", Hint: "write the documented value"},
			want: []string{emptyPathIssue},
		},
		// "must be set" is 11 runes, one below the floor; the Hint answers the condition clause and
		// the phrase is not denied, so only the length guard rejects it.
		usabilityClauseCase{
			name: "eleven runes with every other clause satisfied",
			diag: Error{Path: "value", Msg: "must be set", Hint: "set the field to the documented value"},
			want: []string{shortMessageIssue},
		},
		// 53 runes, a locator, no denied phrase, and no contracted condition or Hint: only the
		// condition clause is left.
		usabilityClauseCase{
			name: "verbose but uninformative",
			diag: Error{Path: "value", Msg: "the supplied input produced an unsatisfactory outcome"},
			want: []string{statesNoConditionIssue},
		},
	)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertUsabilityIssues(t, tc.diag, tc.want)
		})
	}
}

// TestEveryDeniedPhraseHasACaseOfItsOwn keeps the deny-list's coverage quantified over the list
// itself: a phrase added without a row would otherwise be denied by code no case exercises.
func TestEveryDeniedPhraseHasACaseOfItsOwn(t *testing.T) {
	if len(deniedPhraseCases) != len(vacuousDiagnosticMessages) {
		t.Fatalf("%d denied-phrase cases for %d denied phrases %v; add the case with the phrase",
			len(deniedPhraseCases), len(vacuousDiagnosticMessages), vacuousDiagnosticMessages)
	}
	for _, phrase := range vacuousDiagnosticMessages {
		if !slices.ContainsFunc(deniedPhraseCases, func(tc usabilityClauseCase) bool {
			return tc.diag.Msg == phrase
		}) {
			t.Errorf("no case writes the denied phrase %q, so the arm denying it is never exercised",
				phrase)
		}
	}
}

// TestTheUsabilityGateAcceptsWhatItShould is the other side of every clause: a diagnostic that
// satisfies all four must draw no reason at all, or a gate widened until it rejects everything
// would pass every test above.
func TestTheUsabilityGateAcceptsWhatItShould(t *testing.T) {
	for _, tc := range []usabilityClauseCase{
		{
			// Twelve runes exactly: the first length the floor admits, so `<` and `<=` differ
			// here.
			name: "twelve runes, the shortest message the floor admits",
			diag: Error{Rule: R1, Path: "value", Msg: "must equal 1"},
		},
		{
			name: "a hint supplementing a contracted condition",
			diag: Error{Rule: R41, Path: "value", Msg: `unknown field "colums"`,
				Hint: `did you mean "columns"?`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertUsabilityIssues(t, tc.diag, nil)
		})
	}
}

func TestUninformativeMessagesAreRejectedForStatingNoCondition(t *testing.T) {
	messages := []struct{ name, message string }{
		{name: "must", message: "the supplied input must be considered problematic"},
		{name: "should", message: "the supplied input should be considered problematic"},
		{name: "cannot", message: "the supplied input cannot be considered satisfactory"},
		{name: "expected", message: "the supplied input is expected to be considered problematic"},
		{name: "required", message: "the supplied input is required to be considered problematic"},
		{name: "only", message: "the supplied input is only considered problematic"},
		{name: "not", message: "the supplied input is not considered satisfactory"},
		{name: "must substring", message: "the mustard-colored input produced an unsatisfactory outcome"},
	}
	for _, tc := range messages {
		t.Run("modal filler "+tc.name, func(t *testing.T) {
			assertUsabilityIssues(t, Error{Path: "value", Msg: tc.message},
				[]string{statesNoConditionIssue})
		})
	}
	for _, subject := range diagnosticSubjects {
		t.Run("subject alone "+subject, func(t *testing.T) {
			assertUsabilityIssues(t, Error{Path: "value", Msg: subject + " value encountered an issue"},
				[]string{statesNoConditionIssue})
		})
	}
}

// assertUsabilityIssues compares the reasons the gate gave against the exact set the case declares.
// The deny-list's reason carries the phrase it matched, so reasons are matched by prefix; everything
// else is a whole reason.
func assertUsabilityIssues(t *testing.T, diag Error, want []string) {
	t.Helper()

	got := diagnosticUsabilityIssues(diag)
	if len(got) != len(want) {
		t.Fatalf("gate gave %d reasons %v, want exactly %d %v", len(got), got, len(want), want)
	}
	for i, reason := range want {
		if !strings.HasPrefix(got[i], reason) {
			t.Errorf("reason %d is %q, want one opening %q", i, got[i], reason)
		}
	}
}

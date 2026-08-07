package config

import (
	"cmp"
	"slices"
)

// complaint is what makes two diagnostics the same diagnostic: the same objection about
// the same token. Rule is absent, so tagging cannot split one complaint into two, and
// Path is absent, so one broken anchor shared by several listeners is reported once
// rather than once per alias.
type complaint struct {
	file string
	line int
	col  int
	msg  string
}

func (e Error) complaint() complaint {
	return complaint{file: e.File, line: e.Line, col: e.Col, msg: e.Msg}
}

// compareByPosition orders two diagnostics. The declared key is
// (File, Line, Col, Path); Msg then breaks the ties that key leaves open.
//
// That makes the order total over a de-duplicated collection, which is the only
// input it is ever given: de-duplication has already made (File, Line, Col, Msg)
// unique, so two entries cannot tie on Msg as well as on position. Ordering
// therefore never falls back on the order in which rules happened to run, and Rule
// is absent from every level so tagging cannot reorder anything.
func compareByPosition(a, b Error) int {
	if order := cmp.Compare(a.File, b.File); order != 0 {
		return order
	}
	if order := cmp.Compare(a.Line, b.Line); order != 0 {
		return order
	}
	if order := cmp.Compare(a.Col, b.Col); order != 0 {
		return order
	}
	if order := cmp.Compare(a.Path, b.Path); order != 0 {
		return order
	}
	return cmp.Compare(a.Msg, b.Msg)
}

// prefersOver reports whether e is the better representative of a group of duplicate
// diagnostics. It is not the sort predicate -- compareByPosition is -- and it consults
// fields the sort does not, which is why it does not share that name.
//
// The smallest locator wins, so the surviving entry cannot depend on the order the
// duplicates arrived in -- which is what keeps rendered output identical even if a
// future rule reports its findings in an unstable order. Between equal locators the
// diagnostic carrying a remediation hint wins, so de-duplication never discards the
// more helpful of two.
//
// Rule decides last. It cannot perturb rendered output, because it is reached only
// once the two entries agree on every field a renderer reads, so either survivor
// renders identically; what it buys is that Step 13's Rule-based coverage inventory
// gets the same answer whatever order the rules ran in.
func (e Error) prefersOver(other Error) bool {
	if e.Path != other.Path {
		return e.Path < other.Path
	}
	if e.Hint == other.Hint {
		return e.Rule < other.Rule
	}
	if other.Hint == "" {
		return true
	}
	if e.Hint == "" {
		return false
	}
	return e.Hint < other.Hint
}

// normalized returns the diagnostics de-duplicated and then ordered, which is the form
// every caller receives. Those are its two jobs, in that order and only in that order:
// the comparison is total only over a de-duplicated collection, which is why the sort
// lives here rather than anywhere a raw collection could reach it
// (TestOnlyNormalizedAppliesTheDiagnosticOrder). An empty collection normalizes to nil so
// that a successful load returns nothing rather than an empty slice.
func (e Errors) normalized() Errors {
	if len(e) == 0 {
		return nil
	}

	unique := e.deduped()
	// The sort is what undoes the map's iteration order rather than inheriting it.
	slices.SortFunc(unique, compareByPosition)
	return unique
}

// deduped keeps one diagnostic per complaint: one objection about one token is one
// diagnostic, whichever rule or whichever aliasing listener reached it. Which of a group
// survives is prefersOver's decision, and it does not depend on arrival order.
//
// The result is in the map's iteration order, so it is not yet a value any caller may
// see; normalized() orders it.
func (e Errors) deduped() Errors {
	best := make(map[complaint]Error, len(e))
	for _, diag := range e {
		about := diag.complaint()
		if kept, found := best[about]; !found || diag.prefersOver(kept) {
			best[about] = diag
		}
	}

	unique := make(Errors, 0, len(best))
	for _, diag := range best {
		unique = append(unique, diag)
	}
	return unique
}

// normalized returns the warnings with duplicates removed and the remainder
// ordered, by the same keys and the same code path as Errors.
func (w Warnings) normalized() Warnings {
	ordered := asErrors(w).normalized()
	if ordered == nil {
		return nil
	}

	normalized := make(Warnings, len(ordered))
	for i, diag := range ordered {
		normalized[i] = Warning(diag)
	}
	return normalized
}

// normalizedResult is the one boundary every public return passes through: it applies the
// determinism contract to both collections at once, so neither can leave the package
// un-normalized. Load and Parse return through it and nothing else does, which
// TestEveryPublicReturnPathIsNormalized enforces by scanning their return statements.
func normalizedResult(cfg *Config, warnings Warnings, errs Errors) (*Config, Warnings, Errors) {
	return cfg, warnings.normalized(), errs.normalized()
}

// asErrors reinterprets warnings as the shared diagnostic shape, so ordering and
// de-duplication exist in exactly one place.
func asErrors(warnings Warnings) Errors {
	diags := make(Errors, len(warnings))
	for i, warning := range warnings {
		diags[i] = Error(warning)
	}
	return diags
}

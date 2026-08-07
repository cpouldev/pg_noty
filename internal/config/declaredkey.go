package config

// This file is what the contract declares about one key of one mapping level: the shapes its
// value may take, the four ways it can be got wrong together with the rule that reports each of
// them, how much of the value each output surface must hide, and the shape it holds beneath it.
//
// It is separate from what a mapping *level* is (mappinglevel.go) because the two answer different
// questions: a walk asks a level which names it accepts, and asks a key what it requires of the
// one value beneath it.

// keySpec is one legal key of one mapping level.
//
// The four RuleID fields each name the rule that reports one way of getting this key wrong. They
// are rules rather than flags because the diagnostic has to carry the rule the contract assigns
// to the condition, and a flag beside a rule is two declarations that can disagree with nothing
// failing.
type keySpec struct {
	name  string
	kinds nodeKinds
	// presence is the rule that reports this key missing, and is empty for a key the contract
	// leaves optional.
	presence RuleID
	// emptiness is the rule that reports this key's value holding nothing. It is declared per key
	// rather than assumed, because the contract is deliberately asymmetric: `listeners: []` says
	// "this instance manages nothing" and is valid (R22, AC #31), while `operations: {}` is a
	// listener that reacts to no statement and is not (R27). A list whose emptiness no numbered
	// rule names -- `payload.columns`, `signing.secrets` -- declares nothing here and is judged by
	// the rules that also judge its entries.
	emptiness RuleID
	// legality is the rule that reports this key written beneath the wrong sibling, and
	// legalUnder is the one sibling it is written beneath legally. The pair exists because one
	// shape is reached from several keys while the contract permits one of its keys under only
	// one of them: an update can be narrowed to a set of columns, and insert and delete change
	// every column at once (R29).
	legality   RuleID
	legalUnder string
	// shapeHint is what a diagnostic about this key's value having the wrong shape offers, for a
	// key whose accepted shapes are worth spelling out rather than naming. Only `operations` has
	// one, because AC #16 requires its diagnostic to show both accepted forms.
	shapeHint string
	// conversion is the numbered rule that reports a scalar this key's wrapper cannot read. Most
	// conversions are structural RuleDecode failures; the three boolean keys are contract rules.
	conversion RuleID
	// sensitive is the diagnostic-render extent; logSensitivity is the resolved-config log
	// extent. Most declarations set both together through holding. destination.url is the
	// deliberate exception: its source stays visible in diagnostics while its resolved
	// credentials never enter logs.
	sensitive      sensitivity
	logSensitivity sensitivity
	// child is the shape this key's value holds, or the shape of each element when the value
	// is a sequence of mappings. A key whose value holds no declared mapping -- every scalar,
	// and every sequence of scalars -- names noLevel.
	child levelName
}

// key declares one legal key. The chained forms below let the declaration read as the contract
// states it: a mandatory scalar holding a URL password, a mapping of a named shape, a sequence
// of them.
func key(name string, kinds nodeKinds) keySpec {
	return keySpec{name: name, kinds: kinds}
}

// scalars declares a run of plain scalar keys, which is what a level of durations and counts
// is made of.
func scalars(names ...string) []keySpec {
	specs := make([]keySpec, 0, len(names))
	for _, name := range names {
		specs = append(specs, key(name, scalarValue))
	}
	return specs
}

// mandatory marks one of the seven keys the contract requires, naming the rule that reports it
// missing.
func (k keySpec) mandatory(reportedBy RuleID) keySpec {
	k.presence = reportedBy
	return k
}

// holdingSomething marks a key whose value must not be empty, naming the rule that reports it so.
func (k keySpec) holdingSomething(reportedBy RuleID) keySpec {
	k.emptiness = reportedBy
	return k
}

// onlyUnder marks a key that is legal beneath one sibling of its level and nowhere else, naming
// the rule that reports it elsewhere.
func (k keySpec) onlyUnder(sibling string, reportedBy RuleID) keySpec {
	k.legality, k.legalUnder = reportedBy, sibling
	return k
}

// shapedLike gives a wrong-shape diagnostic about this key the hint that shows its accepted forms.
func (k keySpec) shapedLike(hint string) keySpec {
	k.shapeHint = hint
	return k
}

// convertedBy assigns a key's scalar conversion failure to the rule that declares its type.
func (k keySpec) convertedBy(reportedBy RuleID) keySpec {
	k.conversion = reportedBy
	return k
}

// of names the shape this key's value holds.
func (k keySpec) of(child levelName) keySpec {
	k.child = child
	return k
}

// holding declares the same sensitive extent for source diagnostics and resolved-config logs.
func (k keySpec) holding(secret sensitivity) keySpec {
	k.sensitive = secret
	k.logSensitivity = secret
	return k
}

// loggedHolding declares an extent only the resolved-config log must hide.
func (k keySpec) loggedHolding(secret sensitivity) keySpec {
	k.logSensitivity = secret
	return k
}

// required reports whether the contract requires this key, which is what naming the rule that
// reports it missing means.
func (k keySpec) required() bool { return k.presence != noRule }

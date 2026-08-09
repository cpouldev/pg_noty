package config

// This file is half the vocabulary the shape declaration in schema.go is written in: what a
// mapping level is, what may be said about one, and how a consumer asks. What may be said about
// one of its *keys* is declaredkey.go's. Neither holds any part of the shape itself, so a change
// to the contract touches schema.go alone.

// nodeKinds is the set of YAML shapes a key's value may take. It is a set rather than a single
// kind because one key legitimately accepts two: `operations` takes the map form or the
// list-form sugar.
type nodeKinds uint8

const (
	scalarValue nodeKinds = 1 << iota
	mappingValue
	sequenceValue
)

// sensitivity is how much of a value one output surface must hide. The distinction is
// load-bearing in both directions: a connection string hidden wholesale loses useful context,
// while a signing secret hidden in part is not hidden at all.
type sensitivity uint8

const (
	publicValue sensitivity = iota // nothing in this value is a secret
	urlPassword                    // only the password inside a connection string
	entireValue                    // the value as written
)

// levelName identifies one mapping shape. noLevel is the absence of one: the value of a key
// that holds no declared mapping.
type levelName string

const (
	noLevel          levelName = ""
	levelRoot        levelName = "root"
	levelDatabase    levelName = "database"
	levelWorker      levelName = "worker"
	levelRetention   levelName = "retention"
	levelDefaults    levelName = "defaults"
	levelRetry       levelName = "retry"
	levelHeaders     levelName = "headers"
	levelListener    levelName = "listener"
	levelOperations  levelName = "operations"
	levelOperation   levelName = "operation"
	levelPayload     levelName = "payload"
	levelDestination levelName = "destination"
	levelSigning     levelName = "signing"
)

// removedKey is a key the corrected contract moved or dropped, with the hint naming where it
// went. It is declared at the level it used to be written at, so the check that rejects it
// finds the hint without knowing any history.
type removedKey struct {
	name string
	hint string
}

// unknownName is how a level refuses a name it does not declare: the rule that refusal violates,
// and what it offers an author when no declared name is close enough to suggest.
//
// It is declared by the level rather than chosen by the walk, because one level's vocabulary is
// itself a numbered rule -- the three statements an operations mapping may name (R28) -- while
// every other level is closed by R41, "no unknown keys at any nesting level".
type unknownName struct {
	rule RuleID
	hint string
}

// mappingLevel is one mapping of the configuration shape.
type mappingLevel struct {
	// freeForm means every key name is legal here, which is what a header mapping is. Such a
	// level declares no keys, so nothing in it is sensitive either.
	freeForm bool
	// httpFieldNames means the names in this mapping are HTTP field names, so two of them
	// differing only in case are one name (R20). It is separate from freeForm because "any name
	// is legal here" and "these names are compared case-insensitively" are two different
	// statements: a future free-form level need not hold header names, and would then be swept
	// into a rule written for them.
	httpFieldNames bool
	// scope means a missing required key at or beneath this level is anchored on this mapping.
	// The contract names two -- the document root and a listener -- and that is what puts the
	// caret for a missing `database.url` at the top of the file and for a missing
	// `destination.url` on the listener that lacks it (ADR-6, AC #30).
	scope    bool
	keys     []keySpec
	removed  []removedKey
	closedBy unknownName
}

// declaredNames is the names this level declares, in the order the contract states them, which is
// the candidate list a suggestion is drawn from. Deriving it from the table is what keeps a hint
// from going stale as keys are added.
func (l mappingLevel) declaredNames() []string {
	names := make([]string, 0, len(l.keys))
	for _, spec := range l.keys {
		names = append(names, spec.name)
	}
	return names
}

// key finds a declared key by name. A free-form level declares none, so every name there is
// undeclared and none of them is sensitive.
func (l mappingLevel) key(name string) (keySpec, bool) {
	for _, spec := range l.keys {
		if spec.name == name {
			return spec, true
		}
	}
	return keySpec{}, false
}

// removedKey finds the migration hint for a key the contract no longer accepts.
func (l mappingLevel) removedKey(name string) (string, bool) {
	for _, removed := range l.removed {
		if removed.name == name {
			return removed.hint, true
		}
	}
	return "", false
}

// refusalOfAnUndeclaredName is how this level refuses a name it does not declare. A level that
// states no refusal of its own is closed by R41, which is the rule for every level whose key list
// is a vocabulary rather than a rule in its own right.
func (l mappingLevel) refusalOfAnUndeclaredName() unknownName {
	if l.closedBy.rule == noRule {
		return unknownName{rule: R41}
	}
	return l.closedBy
}

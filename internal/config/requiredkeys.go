package config

import (
	"strconv"

	"github.com/goccy/go-yaml/ast"
)

// This file is the seven keys the contract requires, and where a diagnostic about a missing one
// points.
//
// A key that was never written has no token of its own, so ADR-6 anchors it on the enclosing
// mapping's first key -- and *which* enclosing mapping the contract decides rather than the walk:
// the document root for `version`, `database.url` and `listeners`, the listener's own mapping for
// `name`, `table`, `operations` and `destination.url`. Those two are the levels schema.go marks as
// scopes, and each answers for every required key beneath it that no nearer scope owns. Splitting
// them that way is what derives the contract's own 3-and-4 division from the table instead of
// restating it here, so an eighth required key inherits its anchor from where it is declared.
//
// It is also what makes an absent `database` block one diagnostic rather than two (AC #30):
// `database` itself is not required, so the root scope reports the `database.url` that would have
// been inside it and says nothing about the block that would have held it.

// missingKey is one required key the document does not write: the path that names it, relative to
// the scope that will report it, and the rule that reports it.
type missingKey struct {
	path string
	rule RuleID
}

// missingRequiredKey is what an author is told about one. It states the violated rule, so it needs
// no hint: the remedy is to write the key it names.
func missingRequiredKey(path string) fault {
	return fault{message: "missing required key " + strconv.Quote(path)}
}

// reportMissingRequiredKeys names every required key at or beneath this scope that the mapping
// does not write, each anchored on the scope's own first key.
//
// A level that is not a scope reports nothing here: its required keys belong to the scope above
// it, and reporting them at both would put two carets on one missing key.
func (p *shapePass) reportMissingRequiredKeys(level mappingLevel, mapping *ast.MappingNode) {
	if !level.scope {
		return
	}

	for _, missing := range requiredKeysAbsentFrom(level, mapping, "") {
		p.reportAboutMapping(missing.rule, mapping, missingRequiredKey(missing.path))
	}
}

// requiredKeysAbsentFrom is every required key of this level and of the shapes beneath it that
// `written` does not hold, each named by its path from the scope that will report it.
//
// `written` is nil when the key holding this level was not written at all. That is not a defensive
// guard but the case AC #30 turns on: the walk descends into the shape an absent key would have
// held, so the required keys inside it are reported rather than lost with it.
func requiredKeysAbsentFrom(level mappingLevel, written *ast.MappingNode, prefix string) []missingKey {
	var absent []missingKey

	for _, spec := range level.keys {
		path := prefix + spec.name
		value, isWritten := entryValueNamed(written, spec.name)

		if !isWritten {
			if spec.required() {
				absent = append(absent, missingKey{path: path, rule: spec.presence})
			}
			absent = append(absent, requiredKeysAbsentBeneath(spec, nil, path)...)
			continue
		}
		absent = append(absent, requiredKeysAbsentBeneath(spec, value, path)...)
	}
	return absent
}

// requiredKeysAbsentBeneath is what the shape one key declares contributes to its scope's list.
func requiredKeysAbsentBeneath(spec keySpec, value ast.Node, path string) []missingKey {
	child, declared := schemaLevels[spec.child]
	if !declared || child.scope {
		// A scope answers for its own required keys, anchored on itself. Descending past one here
		// would report a listener's missing `name` at the top of the file, and report it once per
		// listener from a mapping that holds none of them.
		return nil
	}
	if value == nil {
		return requiredKeysAbsentFrom(child, nil, path+".")
	}

	mapping, isMapping := beneathNodeProperties(value).(*ast.MappingNode)
	if !isMapping {
		// The shape check has already refused this value. Asking what it holds would report every
		// required key beneath it as missing as well, which is one authoring mistake reported
		// twice over.
		return nil
	}
	return requiredKeysAbsentFrom(child, mapping, path+".")
}

// entryValueNamed is the value a mapping writes under name, and whether it writes one at all.
//
// A nil mapping writes nothing, which is how "the key holding this shape was never written"
// reaches the walk above as an ordinary absence rather than as a case of its own.
func entryValueNamed(mapping *ast.MappingNode, name string) (ast.Node, bool) {
	if mapping == nil {
		return nil, false
	}

	for _, entry := range mapping.Values {
		if text, readable := keyTextOf(entry.Key); readable && text == name {
			return entry.Value, true
		}
	}
	return nil, false
}

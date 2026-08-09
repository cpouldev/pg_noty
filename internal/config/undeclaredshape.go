package config

import "github.com/goccy/go-yaml/ast"

// This file is what the schema walk contributes where the table does not describe what it is
// looking at. schemawalk.go answers for a document shaped the way the contract declares it; every
// arm of that walk which cannot use a declaration hands the node here rather than returning
// nothing, because "nothing is declared here" is the one answer redaction may not default to.
//
// Two different unknowns arrive. A key the table does not *name* is an unknown context, where a
// leaf called `url` may be anything, so the name is consulted and the value taken wholesale. A key
// the table names while the document writes some other *shape* beneath it -- a mapping under a
// declared scalar, a container under a free-form header -- is an unknown context one level down:
// the name is declared and public, and nothing about what was written inside it is. Both end in the
// same descent, and the difference is only whether the name at the top is asked about.

// sensitiveValuesOfUnknownMappingEntry answers one entry of a mapping written in a context the
// schema does not describe.
//
// A key this file cannot read is treated as though it named a sensitive one, because the name it
// decodes to may be one: the same fail-closed answer the fallback branch gives such a key.
func sensitiveValuesOfUnknownMappingEntry(text *source, entry *ast.MappingValueNode) []sensitiveValue {
	name, readable := keyTextOf(entry.Key)
	if readable {
		return sensitiveValuesOfUnknownEntry(text, name, entry.Value)
	}

	located, positioned := locate(text, entry.Value, entireValue)
	if !positioned {
		return nil
	}
	return []sensitiveValue{located}
}

// sensitiveValuesOfUnknownEntry fails closed inside a mapping whose parent the schema does not
// recognise. Sensitive names are still derived from schema.go; the unknown context changes only how
// much must be hidden, so a name such as url is taken wholesale instead of guessed as public or
// password-only.
func sensitiveValuesOfUnknownEntry(text *source, name string, value ast.Node) []sensitiveValue {
	var found []sensitiveValue
	if namesASensitiveKey(name) {
		found = append(found, locateSensitiveValues(text, value, entireValue)...)
		// The whole value is already hidden. Descending would add overlapping
		// replacements whose source coordinates no longer describe the copy after
		// the outer replacement.
		if len(found) != 0 {
			return found
		}
	}
	return append(found, sensitiveValuesBeneathAnUndeclaredShape(text, value)...)
}

// sensitiveValuesBeneathAnUndeclaredShape is what the entries of a container contribute when
// nothing declares what that container is.
//
// It is the arm three separate decisions in schemawalk.go used to answer by returning nothing, and
// each of them was a decision about a *name* silently answering a question about a *shape*: a
// free-form level declares every name public and declares no shape; a declared key naming no child
// level describes a scalar leaf and describes nothing written as a container beneath it; and a
// declared level reached at a node that is not a mapping was written as something the level does
// not describe. All three renders a `url` or a `secrets` beneath them in the clear.
//
// A scalar contributes nothing here on purpose, and that is the bound rather than an omission: the
// name above it *is* declared, and declared public, so a connection string written at
// `worker.concurrency` or as a free-form header value renders exactly as
// TestADeclaredPublicScalarKeepsAConnectionStringWrittenAsItsValue requires.
func sensitiveValuesBeneathAnUndeclaredShape(text *source, value ast.Node) []sensitiveValue {
	var found []sensitiveValue

	switch held := beneathNodeProperties(value).(type) {
	case *ast.MappingNode:
		for _, entry := range held.Values {
			found = append(found, sensitiveValuesOfUnknownMappingEntry(text, entry)...)
		}
	case *ast.SequenceNode:
		// An element has no key of its own, so this recurses rather than going through the
		// key-name arm above with an empty name. That is what it did, and an empty name is not a
		// sensitive one, so the two run the same code -- but a name handed to a function that
		// decides whether a name is sensitive reads as one that might match, inside the component
		// where reading that wrong is a leak.
		for _, entry := range held.Values {
			found = append(found, sensitiveValuesBeneathAnUndeclaredShape(text, entry)...)
		}
	}
	return found
}

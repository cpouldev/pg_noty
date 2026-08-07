package config

import "fmt"

type listEntryFault func(string) (fault, bool)

func (v *validatePass) validateIdentifierList(rule RuleID, list StrList, nonEmpty bool) {
	if !list.Valid() {
		return
	}
	if nonEmpty && len(list.values) == 0 {
		v.reportStructured(rule, list, semanticFault("must hold at least one identifier"))
		return
	}
	v.validateUniqueList(rule, list, identifierEntryFault)
}

func identifierEntryFault(value string) (fault, bool) {
	defect := identifierDefect(value)
	if defect == identOK {
		return fault{}, false
	}
	return semanticFault(fmt.Sprintf(
		"must be a PostgreSQL identifier of at most %d bytes: %s", maxIdentifierBytes, defect)), true
}

func (v *validatePass) validateSigningSecrets(list StrList) {
	if !list.Valid() {
		return
	}
	v.validateUniqueList(R38, list, nonEmptyEntryFault)
}

func nonEmptyEntryFault(value string) (fault, bool) {
	return semanticFault("signing secret must not be empty"), value == ""
}

func (v *validatePass) validateUniqueList(rule RuleID, list StrList, invalid listEntryFault) {
	first := make(map[string]Str, len(list.values))
	for i := range list.values {
		entry := list.values[i]
		if why, broken := invalid(entry.value); broken {
			v.reportListElement(rule, entry, why)
			continue
		}
		if prior, duplicate := first[entry.value]; duplicate {
			v.reportListElement(rule, entry, semanticFault(fmt.Sprintf(
				"entry duplicates the one first written on line %d", prior.Line())))
			continue
		}
		first[entry.value] = entry
	}
}

func (v *validatePass) reportListElement(rule RuleID, entry Str, why fault) {
	reportSemanticUsing(&v.stage, rule, listElementUse, anchorSet{element: entry}, why)
}

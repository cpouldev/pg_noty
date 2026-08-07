package config

import "strings"

var structuralDiagnosticContractSignals = map[RuleID][]diagnosticContractSignal{
	W1: {exactDiagnostic(
		"destination.signing.secrets should use an interpolated environment reference instead of a YAML literal; move the secret to the environment and write ${VARIABLE_NAME}")},
	W2: {exactDiagnostic(
		"worker.drain_timeout is smaller than the largest effective listeners[].timeout; increase the drain timeout or lower the listener timeout so shutdown does not abandon in-flight requests")},

	RuleRead: {
		exactDiagnostic("cannot read configuration file: no such file or directory"),
		exactDiagnostic("cannot read configuration file: is a directory"),
	},
	RuleSyntax: {
		shapedDiagnostic(`found character '.*' that cannot start any token`,
			"found character '\t' that cannot start any token"),
		shapedDiagnostic(`sequence end token '.*' not found`,
			"sequence end token ']' not found"),
		exactDiagnostic(`could not find expected ':' in "already defined at the top"`),
	},
	RuleDocument: {
		exactDiagnostic("configuration file is empty or contains only comments"),
		shapedDiagnostic(
			`configuration file must contain exactly one yaml document, found [2-9][0-9]*`,
			"configuration file must contain exactly one YAML document, found 2"),
		shapedDiagnostic(
			`configuration root must be a mapping, found [a-z]+`,
			"configuration root must be a mapping, found sequence"),
	},
	RuleInterpolate: {
		shapedDiagnostic(
			`environment variable "[a-z_][a-z0-9_]*" is not set`,
			`environment variable "database_url" is not set`),
		exactDiagnostic("malformed environment reference: a variable name is required"),
		exactDiagnostic("malformed environment reference: a variable name must match [A-Za-z_][A-Za-z0-9_]*"),
		exactDiagnostic("malformed environment reference: no closing brace"),
		exactDiagnostic("interpolation is not applied to mapping keys"),
		exactDiagnostic("unsupported YAML shape in a value position"),
		exactDiagnostic("unsupported YAML shape in a key position"),
	},
	RuleNormalize: {
		exactDiagnostic("alias refers to an anchor that is not defined earlier in this document"),
		exactDiagnostic("alias refers to the anchor whose own value it is written inside"),
		exactDiagnostic("a merge key can only merge a mapping"),
		exactDiagnostic("an operations list entry must be a name"),
		shapedDiagnostic(
			`this entry names the operation already written on line [1-9][0-9]*`,
			"this entry names the operation already written on line 8"),
		exactDiagnostic("unsupported YAML shape where an alias could be written"),
	},
	RuleShape: {
		shapedDiagnostic(
			`".*" must be (?:a scalar|a mapping|a list|a mapping or a list)`,
			`"operations" must be a mapping or a list`),
		shapedDiagnostic(
			`each ".*" entry must be (?:a scalar|a mapping|a list)`,
			`each "listeners" entry must be a mapping`),
		shapedDiagnostic(
			`".*" must hold at least one entry`,
			`"operations" must hold at least one entry`),
		exactDiagnostic("unsupported YAML shape in a value position"),
	},
	RuleDecode: {
		exactDiagnostic("expected text"),
		exactDiagnostic("expected an integer"),
		exactDiagnostic("expected true or false"),
	},
}

var diagnosticContractSignals = mergeDiagnosticContracts(
	numberedDiagnosticContractSignals,
	structuralDiagnosticContractSignals,
)

func mergeDiagnosticContracts(
	tables ...map[RuleID][]diagnosticContractSignal,
) map[RuleID][]diagnosticContractSignal {
	merged := make(map[RuleID][]diagnosticContractSignal)
	for _, table := range tables {
		for rule, signals := range table {
			if _, duplicate := merged[rule]; duplicate {
				panic("duplicate diagnostic usability contract for " + rule)
			}
			merged[rule] = signals
		}
	}
	return merged
}

func matchesDiagnosticContract(diagnostic Error) bool {
	message := strings.ToLower(strings.TrimSpace(diagnostic.Msg))
	for _, signal := range diagnosticContractSignals[diagnostic.Rule] {
		if signal.matcher.MatchString(message) {
			return true
		}
	}
	return false
}

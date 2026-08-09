package config

import (
	"slices"
	"strings"
)

// This file declares the shape of the configuration file, and is the only place that shape is
// written down: for every mapping level, its legal keys with the node kinds they accept, which
// of them the contract requires, how much each output surface must hide, and which keys the
// corrected contract moved or removed. Everything else consumes it as data -- the redactor
// here, the structural check and the drift test later -- so adding a key or a sensitive path
// never touches the renderer. What a level and a key *are* is mappinglevel.go's subject.
//
// This table is the mechanism skill Pattern 3 prescribes. That pattern's structural unknown-key
// walk has to be driven by a declared shape rather than by what a decoder happens to accept,
// because a decoder cannot report the key it did *not* recognise with a position; the walk is
// sensitivepaths.go's here and Step 5's for unknown keys, and both read this and nothing else.
//
// Thirteen shapes cover the fifteen paths a mapping is reachable by, because two shapes are
// reached from two places each: `retry`, under `defaults` and under a listener, which is what
// lets a listener override one retry field and inherit the rest; and `headers`, under
// `defaults` and under a `destination`. schema_test.go walks to all fifteen by path, so a
// level that became unreachable fails there even though its declaration still exists.
//
// Each level's keys are in the order the contract states them, so that anything derived from
// the table -- a suggestion's candidate list, a rendered message -- is ordered by the contract
// rather than by a map.
// The two key names the rule code has to say out loud, spelled here beside the declarations that
// give them their meaning so that a rule and the table cannot come to name different keys.
//
// operationsKey is the one key this table accepts in two shapes: the map form, and the list sugar
// stage E folds into it (listsugar.go). updateOperation is the one statement the update-only
// filters apply to.
const (
	operationsKey     = "operations"
	updateOperation   = "update"
	payloadModeFull   = "full"
	payloadModeCols   = "columns"
	payloadModeKeys   = "keys_only"
	payloadColumnsKey = "columns"
)

var (
	retryBackoffs      = []string{"exponential", "linear", "fixed"}
	payloadModes       = []string{payloadModeFull, payloadModeCols, payloadModeKeys}
	destinationMethods = []string{"POST", "PUT", "PATCH"}

	reservedSchemaFault = fault{
		message: reservedSchemaPrefixReason,
		hint:    `use "` + suggestedSchema + `" instead`,
	}
)

const (
	durationDayExample = "7d"
	durationDayHint    = `use "168h" instead`
)

var schemaLevels = map[levelName]mappingLevel{
	levelRoot: {scope: true, keys: []keySpec{
		key("version", scalarValue).mandatory(R1),
		key("instance", scalarValue),
		key("auto_reconcile", scalarValue),
		key("database", mappingValue).of(levelDatabase),
		key("worker", mappingValue).of(levelWorker),
		key("retention", mappingValue).of(levelRetention),
		key("defaults", mappingValue).of(levelDefaults),
		key("listeners", sequenceValue).mandatory(R22).of(levelListener),
	}},
	levelDatabase: {keys: []keySpec{
		key("url", scalarValue).mandatory(R3).holding(urlPassword),
		key("schema", scalarValue),
		key("listen_url", scalarValue).holding(urlPassword),
	}},
	levelWorker: {keys: append(scalars("concurrency", "batch_size", "poll_interval", "lease_timeout", "drain_timeout"),
		key("allowed_destination_cidrs", sequenceValue))},
	levelRetention: {keys: scalars("keep", "partition_interval", "precreate")},
	levelDefaults: {keys: []keySpec{
		key("timeout", scalarValue),
		key("retry", mappingValue).of(levelRetry),
		key("headers", mappingValue).of(levelHeaders),
	}},
	levelRetry: {keys: []keySpec{
		key("max_attempts", scalarValue),
		key("backoff", scalarValue),
		key("initial_interval", scalarValue),
		key("max_interval", scalarValue),
		key("jitter", scalarValue).convertedBy(R18),
	}},
	levelHeaders: {freeForm: true, httpFieldNames: true},
	levelListener: {
		scope: true,
		keys: []keySpec{
			key("name", scalarValue).mandatory(R23),
			key("enabled", scalarValue).convertedBy(R25),
			key("table", scalarValue).mandatory(R26),
			key(operationsKey, mappingValue|sequenceValue).
				mandatory(R27).holdingSomething(R27).shapedLike(bothOperationForms).of(levelOperations),
			key("payload", mappingValue).of(levelPayload),
			key("destination", mappingValue).of(levelDestination),
			key("retry", mappingValue).of(levelRetry),
			key("timeout", scalarValue),
			key("concurrency", scalarValue),
		},
		removed: []removedKey{
			{name: "when", hint: "move it under operations.<op>.when, so it applies to the statement it was written for"},
			{name: "columns", hint: "move it under operations.update.columns, the only statement a column filter applies to"},
		},
	},
	levelOperations: {
		// The three statements are the whole of this level's vocabulary and the contract states
		// them as a rule of their own, so a fourth name is refused by R28 naming them rather than
		// by R41 naming nothing (AC #16).
		closedBy: unknownName{rule: R28, hint: "only insert, update and delete are supported"},
		keys: []keySpec{
			key("insert", mappingValue).of(levelOperation),
			key(updateOperation, mappingValue).of(levelOperation),
			key("delete", mappingValue).of(levelOperation),
		},
	},
	levelOperation: {keys: []keySpec{
		key("columns", sequenceValue).onlyUnder(updateOperation, R29).holdingSomething(R29),
		key("is_distinct", scalarValue).onlyUnder(updateOperation, R43).convertedBy(R43),
		key("when", scalarValue),
	}},
	levelPayload: {keys: []keySpec{
		key("mode", scalarValue),
		key("columns", sequenceValue),
		key("exclude", sequenceValue),
		key("include_old", scalarValue).convertedBy(R34),
		key("max_bytes", scalarValue),
	}},
	levelDestination: {
		keys: []keySpec{
			key("url", scalarValue).mandatory(R36).loggedHolding(urlPassword),
			key("method", scalarValue),
			key("headers", mappingValue).of(levelHeaders),
			key("signing", mappingValue).of(levelSigning),
		},
		removed: []removedKey{
			{name: "type", hint: "HTTP is the only destination kind, so there is nothing left to choose; remove the key"},
		},
	},
	levelSigning: {
		keys: []keySpec{key("secrets", sequenceValue).holding(entireValue)},
		removed: []removedKey{
			{name: "secret", hint: "use signing.secrets, a list, so a secret can be rotated without downtime"},
		},
	},
}

// conversionRuleAt returns the numbered type rule declared for path's leaf key. A key with no
// numbered conversion remains RuleDecode.
//
// It ranges over schemaLevels, which is a map, so it is correct only while no leaf name carries two
// different numbered conversions -- otherwise Go's per-range randomisation would choose which RuleID
// a diagnostic reports. TestNoLeafNameDeclaresTwoNumberedConversions pins that precondition; the
// comment here used to claim "schema tests pin" it without naming one, and none did.
func conversionRuleAt(path string) RuleID {
	leaf := path
	if dot := strings.LastIndexByte(path, '.'); dot >= 0 {
		leaf = path[dot+1:]
	}
	for _, level := range schemaLevels {
		for _, spec := range level.keys {
			if spec.name == leaf && spec.conversion != noRule {
				return spec.conversion
			}
		}
	}
	return noRule
}

// sensitiveKeyNames is the leaf name of every key the table declares sensitive, sorted so that
// no iteration order can reach rendered output. It is derived from the table rather than listed
// a second time, so a newly declared sensitive key is covered by the key-scoped fallback the
// moment it is added.
//
// It carries the over-redaction D3 accepts for that fallback: `url` is the name of
// database.url, which hides a password, and of destination.url, which hides nothing. Only a
// path tells those apart, and the fallback exists precisely for a document that has none.
var sensitiveKeyNames = declaredSensitiveKeyNames()

func declaredSensitiveKeyNames() []string {
	var names []string
	for _, level := range schemaLevels {
		for _, spec := range level.keys {
			if spec.sensitive != publicValue {
				names = append(names, spec.name)
			}
		}
	}

	slices.Sort(names)
	return slices.Compact(names)
}

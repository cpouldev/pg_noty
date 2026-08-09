package config

import (
	"path/filepath"
	"testing"
)

func stagePolicyCases() []stagePolicyCase {
	return []stagePolicyCase{
		{"A", "fatal-single", loadPolicyCase,
			[]policyFinding{{RuleRead, "cannot read configuration file"}}, nil},
		{"B", "fatal-single", syntaxPolicyCase,
			[]policyFinding{{RuleSyntax, "sequence end token"}}, nil},
		{"C", "fatal-single", parseTextPolicy(policyTwoDocuments),
			[]policyFinding{{RuleDocument, "exactly one YAML document"}}, nil},
		{"D", "accumulate-then-stop", parseTextPolicy(policyTwoReferences),
			[]policyFinding{
				{RuleInterpolate, "FIRST_MISSING"}, {RuleInterpolate, "SECOND_MISSING"},
			}, nil},
		{"E", "accumulate-then-stop", parseTextPolicy(policyTwoAliases),
			[]policyFinding{
				{RuleNormalize, "alias refers to an anchor"}, {RuleNormalize, "alias refers to an anchor"},
			}, nil},
		{"F", "accumulate-and-stop-on-wrong-kind", parseTextPolicy(policyWrongKind),
			[]policyFinding{
				{R41, "unknown field"}, {RuleShape, "must be a mapping or a list"},
			}, nil},
		{"G", "cannot-fail", parseTextPolicy(policyConversionContinues),
			[]policyFinding{
				{R1, "must equal 1"}, {RuleDecode, "expected an integer"},
			}, []RuleID{W1, W2}},
		{"H", "accumulate-with-Set-gate", parseTextPolicy(policySetGate),
			[]policyFinding{
				{R1, "missing required key"}, {R7, "range 1 to 10000"},
			}, nil},
		{"I", "accumulate", parseTextPolicy(policyEffectiveComparisons),
			[]policyFinding{
				{R9, "worker.lease_timeout"}, {R11, "retention.keep"},
				{R12, "retention.precreate"}, {R16, "retry.initial_interval"},
				{R39, "listeners[].concurrency"},
			}, []RuleID{W2}},
	}
}

func loadPolicyCase(t *testing.T) policyOutcome {
	path := filepath.Join(t.TempDir(), "absent.yaml")
	cfg, warnings, errs := Load(path, MapEnv(nil))
	return policyOutcome{cfg, warnings, errs}
}

func parseTextPolicy(text string) func(*testing.T) policyOutcome {
	return func(t *testing.T) policyOutcome {
		t.Helper()
		cfg, warnings, errs := Parse([]byte(text), "policy.yaml", MapEnv(nil))
		return policyOutcome{cfg, warnings, errs}
	}
}

func syntaxPolicyCase(t *testing.T) policyOutcome {
	t.Helper()
	path := fixture("policy_syntax_plus_four_semantic")
	data := readFixtureBytes(t, path)
	assertPolicyOutcome(t, repairedSyntaxPolicyOutcome(t, path, data), []policyFinding{
		{R1, "must equal 1"},
		{R41, `unknown field "schmea"`},
		{R7, "range 1 to 10000"},
		{R40, "Go duration"},
	}, nil)
	cfg, warnings, errs := Parse(data, filepath.Base(path), MapEnv(nil))
	return policyOutcome{cfg, warnings, errs}
}

func repairedSyntaxPolicyOutcome(t *testing.T, path string, data []byte) policyOutcome {
	t.Helper()
	repaired := replaceOnce(t, string(data), "listeners: [one, two\n", "listeners: []\n")
	cfg, warnings, errs := Parse([]byte(repaired), filepath.Base(path), MapEnv(nil))
	return policyOutcome{cfg, warnings, errs}
}

const policyTwoDocuments = `version: 1
database: {url: postgres://noty@db/noty}
listeners: []
---
version: 2
`

const policyTwoReferences = `version: 1
database:
  url: ${FIRST_MISSING}
  schmea: ${SECOND_MISSING}
listeners: []
`

const policyTwoAliases = `first: *nowhere
second: *elsewhere
`

const policyWrongKind = `version: 2
rootstray: true
database:
  url: postgres://noty@db.internal/noty
listeners:
  - name: order_paid
    table: public.orders
    operations: insert
    destination:
      url: https://hooks.example.test/order-paid
`

const policyConversionContinues = `version: 2
database:
  url: postgres://noty@db.internal/noty
worker:
  concurrency: nope
  lease_timeout: 20s
  drain_timeout: 9s
listeners:
  - name: order_paid
    table: public.orders
    operations: [insert]
    timeout: 10s
    destination:
      url: https://hooks.example.test/order-paid
      signing:
        secrets: [literal-secret]
`

const policySetGate = `database:
  url: postgres://noty@db.internal/noty
worker:
  batch_size: 0
listeners: []
`

const policyEffectiveComparisons = `version: 1
database:
  url: postgres://noty@db.internal/noty
worker:
  concurrency: 1
  lease_timeout: 5s
  drain_timeout: 1s
retention:
  keep: 30m
  partition_interval: 1h
  precreate: 30m
defaults:
  retry:
    initial_interval: 3s
    max_interval: 2s
listeners:
  - name: order_paid
    table: public.orders
    operations: [insert]
    timeout: 5s
    concurrency: 2
    destination:
      url: https://hooks.example.test/order-paid
`

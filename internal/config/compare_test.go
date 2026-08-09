package config

import (
	"testing"
)

const comparisonBaseline = `version: 1
database:
  url: postgres://noty@db.internal/noty
worker:
  concurrency: 8
  lease_timeout: 11s
  drain_timeout: 20s
retention:
  keep: 2h
  partition_interval: 1h
  precreate: 2h
defaults:
  timeout: 10s
  retry:
    initial_interval: 1s
    max_interval: 2s
listeners:
  - name: order_paid
    table: public.orders
    operations: [insert]
    timeout: 10s
    concurrency: 7
    destination:
      url: https://hooks.example.test/order-paid
`

func TestEveryEffectiveComparisonHasBelowEqualAndAboveCoverage(t *testing.T) {
	tests := []struct {
		name, from, to string
		want           RuleID
	}{
		{"R9 below is rejected", "lease_timeout: 11s", "lease_timeout: 9s", R9},
		{"R9 equal is rejected", "lease_timeout: 11s", "lease_timeout: 10s", R9},
		{"R9 above is accepted", "lease_timeout: 11s", "lease_timeout: 12s", noRule},
		{"R11 below is rejected", "keep: 2h", "keep: 59m", R11},
		{"R11 equal is accepted", "keep: 2h", "keep: 1h", noRule},
		{"R11 above is accepted", "keep: 2h", "keep: 61m", noRule},
		{"R12 below is rejected", "precreate: 2h", "precreate: 59m", R12},
		{"R12 equal is accepted", "precreate: 2h", "precreate: 1h", noRule},
		{"R12 above is accepted", "precreate: 2h", "precreate: 61m", noRule},
		{"R16 zero is rejected", "initial_interval: 1s", "initial_interval: 0s", R16},
		{"R16 below is accepted", "initial_interval: 1s", "initial_interval: 1s", noRule},
		{"R16 equal is accepted", "initial_interval: 1s", "initial_interval: 2s", noRule},
		{"R16 above is rejected", "initial_interval: 1s", "initial_interval: 3s", R16},
		{"R39 below is accepted", "concurrency: 7", "concurrency: 6", noRule},
		{"R39 equal is accepted", "concurrency: 7", "concurrency: 8", noRule},
		{"R39 above is rejected", "concurrency: 7", "concurrency: 9", R39},
		{"R39 invalid endpoint gets only its range error", "concurrency: 7", "concurrency: 1025", R39},
		{"R39 invalid worker endpoint gets only R6", "worker:\n  concurrency: 8", "worker:\n  concurrency: 0", R6},
	}
	raised := map[RuleID]bool{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text := replaceOnce(t, comparisonBaseline, tc.from, tc.to)
			cfg, warnings, errs := Parse([]byte(text), "boundaries.yaml", MapEnv(nil))
			if len(warnings) != 0 {
				t.Fatalf("got warnings %+v, want none", warnings)
			}
			if tc.want == noRule {
				if len(errs) != 0 || cfg == nil {
					t.Fatalf("accepted boundary returned config=%v errors=%+v", cfg, errs)
				}
				return
			}
			if len(errs) != 1 || errs[0].Rule != tc.want || cfg != nil {
				t.Fatalf("returned config=%v errors=%+v, want one %s", cfg, errs, tc.want)
			}
			raised[tc.want] = true
		})
	}
	for rule := range comparisonFixtureManifest {
		if !raised[rule] {
			t.Errorf("%s has no raising boundary", rule)
		}
	}
}

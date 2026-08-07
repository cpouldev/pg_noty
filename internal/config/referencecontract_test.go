package config

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var referenceContractVariables = map[string]string{
	"DATABASE_URL":        "postgresql://noty:reference-db-password@db.internal:5432/noty",
	"DATABASE_DIRECT_URL": "postgresql://noty:reference-direct-password@direct.internal:5432/noty",
	"ORDER_WEBHOOK_URL":   "https://hooks.example.test/order-paid",
	"SIGNING_SECRET":      "reference-signing-current",
	"SIGNING_SECRET_OLD":  "reference-signing-previous",
}

func TestTheExactPrintedContractLoadsWithItsFilters(t *testing.T) {
	data, cfg := loadReferenceContract(t, "reference_contract_filtered_map")
	positions := map[string]operationPositionLiterals{
		"insert": {},
		"update": {
			columns: "[status, total]",
			when:    `"OLD.status <> 'paid' AND NEW.status = 'paid'"`,
		},
	}
	assertReferenceSourcePositions(t, data, "reference_contract_filtered_map.yaml", cfg, positions)
	assertNormalizationOnlyClearsSourcePositions(t, cfg)
	if got, want := semanticConfig(cfg), filteredReferenceContractConfig(); !reflect.DeepEqual(got, want) {
		t.Errorf("filtered contract differs from the exact expected Config:\ngot  %#v\nwant %#v",
			got, want)
	}
}

// The printed map form carries update filters, while list sugar is explicitly
// filter-free. One pair cannot therefore satisfy both "preserve every printed field"
// and "map/list resolve equally". The exact-map test above proves the former; this
// empty-filter pair proves that the sugar itself is semantically exact.
func TestFilterFreeMapAndListSugarResolveEqually(t *testing.T) {
	var resolved []Config
	for _, form := range []string{"map", "list"} {
		t.Run(form, func(t *testing.T) {
			name := "reference_contract_sugar_" + form
			data, cfg := loadReferenceContract(t, name)
			positions := map[string]operationPositionLiterals{"insert": {}, "update": {}}
			assertReferenceSourcePositions(t, data, name+fixtureExtension, cfg, positions)
			assertNormalizationOnlyClearsSourcePositions(t, cfg)
			if got, want := semanticConfig(cfg), filterFreeReferenceContractConfig(); !reflect.DeepEqual(got, want) {
				t.Errorf("%s differs from the exact filter-free Config:\ngot  %#v\nwant %#v",
					form, got, want)
			}
			resolved = append(resolved, cfg)
		})
	}
	if len(resolved) != 2 {
		t.Fatalf("%d sugar forms resolved, want both map and list", len(resolved))
	}
	assertOnlySourcePositionsDiffer(t, resolved[0], resolved[1])
}

func loadReferenceContract(t *testing.T, stem string) ([]byte, Config) {
	t.Helper()
	name := stem + fixtureExtension
	path := filepath.Join(validCorpus, name)
	data := readFixtureBytes(t, path)
	cfg, warnings, errs := Parse(data, name, MapEnv(referenceContractVariables))
	if cfg == nil || len(warnings) != 0 || len(errs) != 0 {
		t.Fatalf("%s returned config=%v warnings=%+v errors=%+v", name, cfg, warnings, errs)
	}
	return data, *cfg
}

func filteredReferenceContractConfig() Config {
	cfg := referenceContractConfig()
	cfg.Listeners[0].Trigger.Operations = Operations{
		{Kind: "insert"},
		{Kind: "update", Columns: []string{"status", "total"},
			When: "OLD.status <> 'paid' AND NEW.status = 'paid'"},
	}
	return cfg
}

func filterFreeReferenceContractConfig() Config {
	cfg := referenceContractConfig()
	cfg.Listeners[0].Trigger.Operations = Operations{{Kind: "insert"}, {Kind: "update"}}
	return cfg
}

func referenceContractConfig() Config {
	return Config{
		Version: 1, Instance: "prod",
		Database: Database{
			URL: referenceContractVariables["DATABASE_URL"], Schema: "noty",
			ListenURL: referenceContractVariables["DATABASE_DIRECT_URL"],
		},
		Worker: Worker{
			Concurrency: 16, BatchSize: 100, PollInterval: 10 * time.Second,
			LeaseTimeout: 5 * time.Minute, DrainTimeout: 30 * time.Second,
		},
		Retention: Retention{
			Keep: 168 * time.Hour, PartitionInterval: 24 * time.Hour, Precreate: 168 * time.Hour,
		},
		Listeners: []Listener{{
			Name: "order_paid", Enabled: true,
			Trigger: TriggerSpec{
				Table: "public.orders",
				Payload: Payload{
					Mode: "full", Exclude: []string{"card_token"}, IncludeOld: true, MaxBytes: 262144,
				},
			},
			Delivery: DeliverySpec{
				Destination: Destination{
					URL: referenceContractVariables["ORDER_WEBHOOK_URL"], Method: "POST",
					Headers: Headers{"User-Agent": "pg_noty/1", "X-Tenant": "acme"},
					Signing: Signing{Secrets: []string{
						referenceContractVariables["SIGNING_SECRET"],
						referenceContractVariables["SIGNING_SECRET_OLD"],
					}},
				},
				Retry: Retry{
					MaxAttempts: 10, Backoff: "exponential", InitialInterval: 10 * time.Second,
					MaxInterval: time.Hour, Jitter: true,
				},
				Timeout: 5 * time.Second, Concurrency: 4,
			},
		}},
	}
}

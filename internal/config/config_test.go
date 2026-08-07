package config

import (
	"reflect"
	"testing"
)

type resolvedTypeContractCase struct {
	name       string
	value      any
	wantFields map[string]string
}

// TestResolvedRootTypesMatchTheDataModelContract covers the process-wide configuration.
// Five later phases bind to these names, so a rename or retyped field must fail here.
func TestResolvedRootTypesMatchTheDataModelContract(t *testing.T) {
	assertResolvedTypeContracts(t, []resolvedTypeContractCase{
		{
			name:  "Config",
			value: Config{},
			wantFields: map[string]string{
				"Version":       "int",
				"Instance":      "string",
				"AutoReconcile": "bool",
				"Database":      "config.Database",
				"Worker":        "config.Worker",
				"Retention":     "config.Retention",
				"Listeners":     "[]config.Listener",
			},
		},
		{
			name:  "Database",
			value: Database{},
			wantFields: map[string]string{
				"URL":       "string",
				"Schema":    "string",
				"ListenURL": "string",
			},
		},
		{
			name:  "Worker",
			value: Worker{},
			wantFields: map[string]string{
				"Concurrency":             "int",
				"BatchSize":               "int",
				"PollInterval":            "time.Duration",
				"LeaseTimeout":            "time.Duration",
				"DrainTimeout":            "time.Duration",
				"AllowedDestinationCIDRs": "[]string",
			},
		},
		{
			name:  "Retention",
			value: Retention{},
			wantFields: map[string]string{
				"Keep":              "time.Duration",
				"PartitionInterval": "time.Duration",
				"Precreate":         "time.Duration",
			},
		},
	})
}

// TestResolvedListenerTypesMatchTheDataModelContract covers the listener envelope and boundaries.
func TestResolvedListenerTypesMatchTheDataModelContract(t *testing.T) {
	assertResolvedTypeContracts(t, []resolvedTypeContractCase{
		{
			name:  "Listener",
			value: Listener{},
			wantFields: map[string]string{
				"Name":     "string",
				"Enabled":  "bool",
				"Trigger":  "config.TriggerSpec",
				"Delivery": "config.DeliverySpec",
			},
		},
		{
			name:  "TriggerSpec",
			value: TriggerSpec{},
			wantFields: map[string]string{
				"Table":      "string",
				"Operations": "config.Operations",
				"Payload":    "config.Payload",
			},
		},
		{
			name:  "DeliverySpec",
			value: DeliverySpec{},
			wantFields: map[string]string{
				"Destination": "config.Destination",
				"Retry":       "config.Retry",
				"Timeout":     "time.Duration",
				"Concurrency": "int",
			},
		},
		{
			name:  "Payload",
			value: Payload{},
			wantFields: map[string]string{
				"Mode":       "string",
				"Columns":    "[]string",
				"Exclude":    "[]string",
				"IncludeOld": "bool",
				"MaxBytes":   "int",
			},
		},
	})
}

// TestResolvedDeliveryAndOperationTypesMatchTheDataModelContract covers the leaf value types.
func TestResolvedDeliveryAndOperationTypesMatchTheDataModelContract(t *testing.T) {
	assertResolvedTypeContracts(t, []resolvedTypeContractCase{
		{
			name:  "Destination",
			value: Destination{},
			wantFields: map[string]string{
				"URL":     "string",
				"Method":  "string",
				"Headers": "config.Headers",
				"Signing": "config.Signing",
			},
		},
		{
			name:       "Signing",
			value:      Signing{},
			wantFields: map[string]string{"Secrets": "[]string"},
		},
		{
			name:  "Retry",
			value: Retry{},
			wantFields: map[string]string{
				"MaxAttempts":     "int",
				"Backoff":         "string",
				"InitialInterval": "time.Duration",
				"MaxInterval":     "time.Duration",
				"Jitter":          "bool",
			},
		},
		{
			name:  "Operation",
			value: Operation{},
			wantFields: map[string]string{
				"Kind":    "string",
				"Columns": "[]string",
				"When":    "string",
			},
		},
	})
}

func assertResolvedTypeContracts(t *testing.T, tests []resolvedTypeContractCase) {
	t.Helper()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertFields(t, reflect.TypeOf(tc.value), tc.wantFields)
		})
	}
}

func assertFields(t *testing.T, typ reflect.Type, want map[string]string) {
	t.Helper()

	got := make(map[string]string, typ.NumField())
	for i := range typ.NumField() {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		got[field.Name] = field.Type.String()
	}

	for name, wantType := range want {
		gotType, ok := got[name]
		if !ok {
			t.Errorf("%s has no field %s", typ.Name(), name)
			continue
		}
		if gotType != wantType {
			t.Errorf("%s.%s is %s, want %s", typ.Name(), name, gotType, wantType)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s has undeclared field %s; the contract lists exactly %d fields", typ.Name(), name, len(want))
		}
	}
}

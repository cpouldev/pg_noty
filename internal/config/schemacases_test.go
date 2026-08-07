package config

type mappingLevelCase struct {
	level    string
	path     []string
	freeForm bool
	keys     []string
}

func mappingLevelCases() []mappingLevelCase {
	return append(rootMappingLevelCases(), listenerMappingLevelCases()...)
}

func rootMappingLevelCases() []mappingLevelCase {
	return []mappingLevelCase{
		{
			level: "root",
			keys:  []string{"version", "instance", "auto_reconcile", "database", "worker", "retention", "defaults", "listeners"},
		},
		{
			level: "database", path: []string{"database"},
			keys: []string{"url", "schema", "listen_url"},
		},
		{
			level: "worker", path: []string{"worker"},
			keys: []string{"concurrency", "batch_size", "poll_interval", "lease_timeout", "drain_timeout",
				"allowed_destination_cidrs"},
		},
		{
			level: "retention", path: []string{"retention"},
			keys: []string{"keep", "partition_interval", "precreate"},
		},
		{
			level: "defaults", path: []string{"defaults"},
			keys: []string{"timeout", "retry", "headers"},
		},
		{
			level: "defaults.retry", path: []string{"defaults", "retry"},
			keys: []string{"max_attempts", "backoff", "initial_interval", "max_interval", "jitter"},
		},
		{
			level: "defaults.headers", path: []string{"defaults", "headers"}, freeForm: true,
		},
	}
}

func listenerMappingLevelCases() []mappingLevelCase {
	return []mappingLevelCase{
		{
			level: "listeners[]", path: []string{"listeners"},
			keys: []string{"name", "enabled", "table", "operations", "payload",
				"destination", "retry", "timeout", "concurrency"},
		},
		{
			level: "operations", path: []string{"listeners", "operations"},
			keys: []string{"insert", "update", "delete"},
		},
		{
			level: "operations.insert", path: []string{"listeners", "operations", "insert"},
			keys: []string{"columns", "when"},
		},
		{
			level: "operations.update", path: []string{"listeners", "operations", "update"},
			keys: []string{"columns", "when"},
		},
		{
			level: "operations.delete", path: []string{"listeners", "operations", "delete"},
			keys: []string{"columns", "when"},
		},
		{
			level: "payload", path: []string{"listeners", "payload"},
			keys: []string{"mode", "columns", "exclude", "include_old", "max_bytes"},
		},
		{
			level: "destination", path: []string{"listeners", "destination"},
			keys: []string{"url", "method", "headers", "signing"},
		},
		{
			level: "destination.headers",
			path:  []string{"listeners", "destination", "headers"}, freeForm: true,
		},
		{
			level: "destination.signing",
			path:  []string{"listeners", "destination", "signing"}, keys: []string{"secrets"},
		},
		{
			level: "listeners[].retry", path: []string{"listeners", "retry"},
			keys: []string{"max_attempts", "backoff", "initial_interval", "max_interval", "jitter"},
		},
	}
}

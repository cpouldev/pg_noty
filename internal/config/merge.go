package config

import (
	"slices"
	"time"
)

// resolveConfig is stage I's three-layer field-level resolution. It follows skill Pattern 7, using
// Pattern 6's Set flag as presence: built-ins are copied first, defaults override one field at a
// time, and each listener overrides one field at a time. No merge decision reads a value or Valid.
func resolveConfig(raw *rawConfig) *Config {
	defaults := resolvedBuiltInDefaults()
	database, instance := resolveDatabase(raw, defaults)
	resolved := &Config{
		Version:       mergedInt(raw.Version, 0),
		Instance:      instance,
		AutoReconcile: mergedBool(raw.AutoReconcile, defaults.autoReconcile),
		Database:      database,
		Worker:        resolveWorker(defaults.worker, raw.Worker.Value),
		Retention:     resolveRetention(defaults.retention, raw.Retention.Value),
		Listeners:     make([]Listener, 0, len(raw.Listeners.Values)),
	}

	listenerDefaults := defaults.listener
	listenerDefaults.timeout = mergedDuration(raw.Defaults.Value.Timeout, listenerDefaults.timeout)
	listenerDefaults.retry = mergeRetry(listenerDefaults.retry, raw.Defaults.Value.Retry.Value)
	for _, listener := range raw.Listeners.Values {
		resolved.Listeners = append(resolved.Listeners,
			resolveListener(listener, raw.Defaults.Value.Headers, listenerDefaults))
	}
	return resolved
}

func resolveDatabase(raw *rawConfig, defaults defaultState) (Database, string) {
	resolved := defaults.database
	resolved.URL = mergedString(raw.Database.Value.URL, "")
	resolved.Schema = mergedString(raw.Database.Value.Schema, resolved.Schema)
	resolved.ListenURL = mergedString(raw.Database.Value.ListenURL, "")
	instance := mergedString(raw.Instance, defaults.instance(resolved.Schema))
	return resolved, instance
}

func resolveWorker(inherited Worker, raw rawWorker) Worker {
	inherited.Concurrency = mergedInt(raw.Concurrency, inherited.Concurrency)
	inherited.BatchSize = mergedInt(raw.BatchSize, inherited.BatchSize)
	inherited.PollInterval = mergedDuration(raw.PollInterval, inherited.PollInterval)
	inherited.LeaseTimeout = mergedDuration(raw.LeaseTimeout, inherited.LeaseTimeout)
	inherited.DrainTimeout = mergedDuration(raw.DrainTimeout, inherited.DrainTimeout)
	inherited.AllowedDestinationCIDRs = mergedStrings(raw.AllowedDestinationCIDRs, inherited.AllowedDestinationCIDRs)
	return inherited
}

func resolveRetention(inherited Retention, raw rawRetention) Retention {
	inherited.Keep = mergedDuration(raw.Keep, inherited.Keep)
	inherited.PartitionInterval = mergedDuration(raw.PartitionInterval, inherited.PartitionInterval)
	inherited.Precreate = mergedDuration(raw.Precreate, inherited.Precreate)
	return inherited
}

func resolveListener(raw rawListener, inheritedHeaders rawHeaders, defaults listenerDefaults) Listener {
	destination := raw.Destination.Value
	listener := Listener{
		Name:    mergedString(raw.Name, ""),
		Enabled: mergedBool(raw.Enabled, defaults.enabled),
		Trigger: TriggerSpec{
			Table:         mergedString(raw.Table, ""),
			Operations:    resolveOperations(raw.Operations),
			Payload:       resolvePayload(raw.Payload.Value, defaults.payload),
			tablePosition: raw.Table.where(),
		},
		Delivery: DeliverySpec{
			Destination: Destination{
				URL:     mergedString(destination.URL, ""),
				Method:  mergedString(destination.Method, defaults.destinationMethod),
				Headers: mergeHeaders(inheritedHeaders, destination.Headers),
				Signing: Signing{Secrets: mergedStrings(destination.Signing.Value.Secrets, nil)},
			},
			Retry:       mergeRetry(defaults.retry, raw.Retry.Value),
			Timeout:     mergedDuration(raw.Timeout, defaults.timeout),
			Concurrency: mergedInt(raw.Concurrency, 0),
		},
	}
	return listener
}

func resolvePayload(raw rawPayload, inherited Payload) Payload {
	inherited.Mode = mergedString(raw.Mode, inherited.Mode)
	inherited.Columns = mergedStrings(raw.Columns, inherited.Columns)
	inherited.Exclude = mergedStrings(raw.Exclude, inherited.Exclude)
	inherited.IncludeOld = mergedBool(raw.IncludeOld, inherited.IncludeOld)
	inherited.MaxBytes = mergedInt(raw.MaxBytes, inherited.MaxBytes)
	return inherited
}

func mergeRetry(inherited Retry, raw rawRetry) Retry {
	inherited.MaxAttempts = mergedInt(raw.MaxAttempts, inherited.MaxAttempts)
	inherited.Backoff = mergedString(raw.Backoff, inherited.Backoff)
	inherited.InitialInterval = mergedDuration(raw.InitialInterval, inherited.InitialInterval)
	inherited.MaxInterval = mergedDuration(raw.MaxInterval, inherited.MaxInterval)
	inherited.Jitter = mergedBool(raw.Jitter, inherited.Jitter)
	return inherited
}

func resolveOperations(raw rawOperations) Operations {
	resolved := make(Operations, 0, len(raw.values))
	for _, operation := range raw.values {
		resolved = append(resolved, Operation{
			Kind:            mergedString(operation.Name, ""),
			Columns:         mergedStrings(operation.Filter.Columns, nil),
			When:            mergedString(operation.Filter.When, ""),
			columnsPosition: operation.Filter.Columns.where(),
			whenPosition:    operation.Filter.When.where(),
		})
	}
	slices.SortStableFunc(resolved, func(a, b Operation) int {
		return compareOperationNames(a.Kind, b.Kind)
	})
	return resolved
}

func mergeHeaders(layers ...rawHeaders) Headers {
	resolved := Headers{}
	for _, layer := range layers {
		if !layer.Set {
			continue
		}
		for _, header := range layer.values {
			name, valid := canonicalHTTPFieldName(mergedString(header.Name, ""))
			if !valid {
				continue
			}
			resolved[name] = mergedString(header.Value, "")
		}
	}
	return resolved
}

func mergedString(raw Str, inherited string) string {
	if raw.Set {
		return raw.value
	}
	return inherited
}

func mergedInt(raw Int, inherited int) int {
	if raw.Set {
		return raw.value
	}
	return inherited
}

func mergedBool(raw Bool, inherited bool) bool {
	if raw.Set {
		return raw.value
	}
	return inherited
}

func mergedDuration(raw Dur, inherited time.Duration) time.Duration {
	if raw.Set {
		return raw.value
	}
	return inherited
}

func mergedStrings(raw StrList, inherited []string) []string {
	if !raw.Set {
		return slices.Clone(inherited)
	}
	resolved := make([]string, 0, len(raw.values))
	for _, value := range raw.values {
		resolved = append(resolved, value.value)
	}
	return resolved
}

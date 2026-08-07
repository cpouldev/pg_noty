package config

import (
	"slices"
	"time"
)

func acceptanceSpecsR39ToR42() []acceptanceSpec {
	return []acceptanceSpec{
		halfAcceptance(R39, r39RangeHalf, "R39_ok_listener_concurrency_boundaries",
			[]acceptanceSubject{
				scalarSubject("listener.concurrency.min",
					"$.listeners[0].concurrency", "1", "2"),
			},
			[]acceptanceObservation{
				configObservation("listener.concurrency.min", "listener concurrency 1",
					func(c Config) bool {
						return len(c.Listeners) == 2 &&
							c.Listeners[0].Delivery.Concurrency == 1
					}),
			}),
		listenerConcurrencyComparisonAcceptance(),
		durationUnitsAcceptance(),
		freeFormHeadersAcceptance(),
		distinctMappingKeysAcceptance(),
	}
}

func listenerConcurrencyComparisonAcceptance() acceptanceSpec {
	const worker = "$.worker.concurrency"
	const listener = "$.listeners[1].concurrency"
	return halfAcceptance(R39, r39CompareHalf, "R39_ok_listener_concurrency_boundaries",
		[]acceptanceSubject{
			scalarSubjectUsing("worker.concurrency", worker, "1024",
				combineMutations(mutateScalar(worker, "1023"), mutateScalar(listener, "1023"))),
			scalarSubject("listener.concurrency.equal", listener, "1024", "1023"),
		},
		[]acceptanceObservation{
			configObservation("worker.concurrency", "worker concurrency 1024",
				func(c Config) bool { return c.Worker.Concurrency == 1024 }),
			configObservation("listener.concurrency.equal", "listener concurrency equals 1024",
				func(c Config) bool {
					return len(c.Listeners) == 2 &&
						c.Listeners[1].Delivery.Concurrency == 1024
				}),
		})
}

func durationUnitsAcceptance() acceptanceSpec {
	return wholeAcceptance(R40, "R40_ok_duration_boundaries",
		[]acceptanceSubject{
			scalarSubject("worker.poll_interval",
				"$.worker.poll_interval", "500µs", "600µs"),
			scalarSubject("worker.lease_timeout",
				"$.worker.lease_timeout", "2h", "3h"),
			scalarSubject("worker.drain_timeout",
				"$.worker.drain_timeout", "2h", "3h"),
			scalarSubject("retention.keep", "$.retention.keep", "168h", "169h"),
			scalarSubject("defaults.timeout", "$.defaults.timeout", "1h30m", "1h29m"),
		},
		[]acceptanceObservation{
			configObservation("worker.poll_interval", "500 microseconds",
				func(c Config) bool {
					return c.Worker.PollInterval == 500*time.Microsecond
				}),
			configObservation("worker.lease_timeout", "the 2h lease prerequisite",
				func(c Config) bool { return c.Worker.LeaseTimeout == 2*time.Hour }),
			configObservation("worker.drain_timeout", "the 2h drain prerequisite",
				func(c Config) bool { return c.Worker.DrainTimeout == 2*time.Hour }),
			configObservation("retention.keep", "the 168h duration",
				func(c Config) bool { return c.Retention.Keep == 168*time.Hour }),
			configObservation("defaults.timeout", "the decoded effective 1h30m timeout",
				func(c Config) bool {
					return len(c.Listeners) == 1 &&
						c.Listeners[0].Delivery.Timeout == 90*time.Minute
				}),
		})
}

func freeFormHeadersAcceptance() acceptanceSpec {
	return wholeAcceptance(R41, "R41_ok_free_form_headers",
		[]acceptanceSubject{
			scalarSubjectUsing("defaults.header.free-form",
				"$.defaults.headers.Not-A-Declared-Key", "2",
				replaceText("defaults:\n  headers:\n    Not-A-Declared-Key: 2\n", "")),
			scalarSubjectUsing("listener.header.free-form",
				"$.listeners[0].destination.headers.Whatever-The-Author-Needs", "yes",
				replaceText("      headers:\n        Whatever-The-Author-Needs: yes\n", "")),
		},
		[]acceptanceObservation{
			configObservation("defaults.header.free-form", "the default free-form header",
				func(c Config) bool {
					return c.Listeners[0].Delivery.Destination.Headers["Not-A-Declared-Key"] == "2"
				}),
			configObservation("listener.header.free-form", "the listener free-form header",
				func(c Config) bool {
					return c.Listeners[0].Delivery.Destination.Headers["Whatever-The-Author-Needs"] == "yes"
				}),
		})
}

func distinctMappingKeysAcceptance() acceptanceSpec {
	return wholeAcceptance(R42, "R42_ok_distinct_mapping_keys",
		[]acceptanceSubject{
			mappingKeySubject("database.schema.key", "$.database", "schema"),
			mappingKeySubject("worker.batch_size.key", "$.worker", "batch_size"),
			mappingKeySubject("operations.insert.key", "$.listeners[0].operations", "insert"),
			mappingKeySubject("operations.update.key", "$.listeners[0].operations", "update"),
		},
		[]acceptanceObservation{
			positionedKeyObservation("database.schema.key", "$.database", "schema"),
			positionedKeyObservation("worker.batch_size.key", "$.worker", "batch_size"),
			positionedKeyObservation("operations.insert.key", "$.listeners[0].operations", "insert"),
			positionedKeyObservation("operations.update.key", "$.listeners[0].operations", "update"),
		})
}

func acceptanceWarningSpecs() []acceptanceSpec {
	return []acceptanceSpec{
		wholeAcceptance(W1, "W1_ok_environment_secret",
			[]acceptanceSubject{
				scalarSubject("signing.secret.environment",
					"$.listeners[0].destination.signing.secrets[0]",
					"${SIGNING_SECRET}", "${SIGNING_SECRET_OLD}"),
			},
			[]acceptanceObservation{
				configObservation("signing.secret.environment", "the environment-origin secret",
					func(c Config) bool {
						return len(c.Listeners) == 1 &&
							slices.Equal(c.Listeners[0].Delivery.Destination.Signing.Secrets,
								[]string{corpusVariables["SIGNING_SECRET"]})
					}),
			}),
		drainTimeoutWarningBoundaryAcceptance(),
	}
}

func drainTimeoutWarningBoundaryAcceptance() acceptanceSpec {
	return wholeAcceptance(W2, "W2_ok_drain_equals_timeout",
		[]acceptanceSubject{
			scalarSubject("worker.drain_timeout", "$.worker.drain_timeout", "10s", "11s"),
			scalarSubject("listener.timeout", "$.listeners[0].timeout", "10s", "9s"),
		},
		[]acceptanceObservation{
			configObservation("worker.drain_timeout", "drain timeout 10s",
				func(c Config) bool { return c.Worker.DrainTimeout == 10*time.Second }),
			configObservation("listener.timeout", "listener timeout 10s",
				func(c Config) bool {
					return len(c.Listeners) == 1 &&
						c.Listeners[0].Delivery.Timeout == 10*time.Second
				}),
		})
}

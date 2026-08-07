package config

import "time"

func acceptanceSpecsR11ToR21() []acceptanceSpec {
	return []acceptanceSpec{
		retentionKeepComparisonAcceptance(),
		retentionPrecreateComparisonAcceptance(),
		timeoutExtentsAcceptance(),
		maxAttemptsExtentsAcceptance(),
		backoffExtentsAcceptance(),
		retryIntervalComparisonAcceptance(),
		maxIntervalExtentsAcceptance(),
		jitterExtentsAcceptance(),
		headerMergeAcceptance(),
		headerIdentityAcceptance(),
		nonReservedHeaderAcceptance(),
	}
}

func retentionKeepComparisonAcceptance() acceptanceSpec {
	return wholeAcceptance(R11, "R11_ok_retention_keep_equals_partition_interval",
		[]acceptanceSubject{
			scalarSubject("retention.keep", "$.retention.keep", "24h", "25h"),
			scalarSubject("retention.partition_interval",
				"$.retention.partition_interval", "24h", "23h"),
		},
		[]acceptanceObservation{
			configObservation("retention.keep", "keep 24h",
				func(c Config) bool { return c.Retention.Keep == 24*time.Hour }),
			configObservation("retention.partition_interval", "partition interval 24h",
				func(c Config) bool { return c.Retention.PartitionInterval == 24*time.Hour }),
		})
}

func retentionPrecreateComparisonAcceptance() acceptanceSpec {
	return wholeAcceptance(R12, "R12_ok_retention_precreate_equals_partition_interval",
		[]acceptanceSubject{
			scalarSubject("retention.precreate", "$.retention.precreate", "24h", "25h"),
			scalarSubject("retention.partition_interval",
				"$.retention.partition_interval", "24h", "23h"),
		},
		[]acceptanceObservation{
			configObservation("retention.precreate", "precreate 24h",
				func(c Config) bool { return c.Retention.Precreate == 24*time.Hour }),
			configObservation("retention.partition_interval", "partition interval 24h",
				func(c Config) bool { return c.Retention.PartitionInterval == 24*time.Hour }),
		})
}

func timeoutExtentsAcceptance() acceptanceSpec {
	return wholeAcceptance(R13, "R13_ok_timeouts_positive",
		[]acceptanceSubject{
			scalarSubject("defaults.timeout", "$.defaults.timeout", "1h30m", "1h29m"),
			scalarSubject("listener.timeout", "$.listeners[0].timeout", "500ms", "600ms"),
		},
		[]acceptanceObservation{
			configObservation("defaults.timeout", "the inheriting listener timeout 1h30m",
				func(c Config) bool {
					return len(c.Listeners) == 2 &&
						c.Listeners[1].Delivery.Timeout == 90*time.Minute
				}),
			configObservation("listener.timeout", "the overriding listener timeout 500ms",
				func(c Config) bool {
					return len(c.Listeners) == 2 &&
						c.Listeners[0].Delivery.Timeout == 500*time.Millisecond
				}),
		})
}

func maxAttemptsExtentsAcceptance() acceptanceSpec {
	return wholeAcceptance(R14, "R14_ok_retry_max_attempts_one",
		[]acceptanceSubject{
			scalarSubject("defaults.max_attempts",
				"$.defaults.retry.max_attempts", "1", "3"),
			scalarSubject("listener.max_attempts",
				"$.listeners[0].retry.max_attempts", "1", "2"),
		},
		[]acceptanceObservation{
			configObservation("defaults.max_attempts", "the inheriting listener max_attempts 1",
				func(c Config) bool {
					return len(c.Listeners) == 2 &&
						c.Listeners[1].Delivery.Retry.MaxAttempts == 1
				}),
			configObservation("listener.max_attempts", "the overriding listener max_attempts 1",
				func(c Config) bool {
					return len(c.Listeners) == 2 &&
						c.Listeners[0].Delivery.Retry.MaxAttempts == 1
				}),
		})
}

func backoffExtentsAcceptance() acceptanceSpec {
	return wholeAcceptance(R15, "R15_ok_retry_backoffs",
		[]acceptanceSubject{
			scalarSubject("defaults.exponential",
				"$.defaults.retry.backoff", "exponential", "fixed"),
			scalarSubject("listener.linear",
				"$.listeners[0].retry.backoff", "linear", "exponential"),
			scalarSubject("listener.fixed",
				"$.listeners[1].retry.backoff", "fixed", "linear"),
		},
		[]acceptanceObservation{
			configObservation("defaults.exponential", "the inheriting listener exponential backoff",
				func(c Config) bool {
					return len(c.Listeners) == 3 &&
						c.Listeners[2].Delivery.Retry.Backoff == "exponential"
				}),
			configObservation("listener.linear", "the linear override",
				func(c Config) bool {
					return len(c.Listeners) == 3 &&
						c.Listeners[0].Delivery.Retry.Backoff == "linear"
				}),
			configObservation("listener.fixed", "the fixed override",
				func(c Config) bool {
					return len(c.Listeners) == 3 &&
						c.Listeners[1].Delivery.Retry.Backoff == "fixed"
				}),
		})
}

func retryIntervalComparisonAcceptance() acceptanceSpec {
	return wholeAcceptance(R16, "R16_ok_retry_initial_equals_max",
		[]acceptanceSubject{
			scalarSubject("retry.initial_interval",
				"$.defaults.retry.initial_interval", "2s", "1s"),
			scalarSubject("retry.max_interval",
				"$.defaults.retry.max_interval", "2s", "3s"),
		},
		[]acceptanceObservation{
			configObservation("retry.initial_interval", "initial interval 2s",
				func(c Config) bool {
					return c.Listeners[0].Delivery.Retry.InitialInterval == 2*time.Second
				}),
			configObservation("retry.max_interval", "max interval 2s",
				func(c Config) bool {
					return c.Listeners[0].Delivery.Retry.MaxInterval == 2*time.Second
				}),
		})
}

func maxIntervalExtentsAcceptance() acceptanceSpec {
	return wholeAcceptance(R17, "R17_ok_retry_max_interval_positive",
		[]acceptanceSubject{
			scalarSubject("defaults.max_interval",
				"$.defaults.retry.max_interval", "2s", "4s"),
			scalarSubject("listener.max_interval",
				"$.listeners[0].retry.max_interval", "3s", "4s"),
		},
		[]acceptanceObservation{
			configObservation("defaults.max_interval", "the inheriting listener max interval 2s",
				func(c Config) bool {
					return len(c.Listeners) == 2 &&
						c.Listeners[1].Delivery.Retry.MaxInterval == 2*time.Second
				}),
			configObservation("listener.max_interval", "the overriding listener max interval 3s",
				func(c Config) bool {
					return len(c.Listeners) == 2 &&
						c.Listeners[0].Delivery.Retry.MaxInterval == 3*time.Second
				}),
		})
}

func jitterExtentsAcceptance() acceptanceSpec {
	return wholeAcceptance(R18, "R18_ok_retry_jitter_boolean",
		[]acceptanceSubject{
			scalarSubject("defaults.jitter.true", "$.defaults.retry.jitter", "true", "false"),
			scalarSubject("listener.jitter.false",
				"$.listeners[0].retry.jitter", "false", "true"),
		},
		[]acceptanceObservation{
			configObservation("defaults.jitter.true", "the inheriting listener jitter true",
				func(c Config) bool {
					return len(c.Listeners) == 2 && c.Listeners[1].Delivery.Retry.Jitter
				}),
			configObservation("listener.jitter.false", "the overriding listener jitter false",
				func(c Config) bool {
					return len(c.Listeners) == 2 && !c.Listeners[0].Delivery.Retry.Jitter
				}),
		})
}

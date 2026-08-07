package config

import (
	"strings"
	"time"
)

func acceptanceSpecsR1ToR7() []acceptanceSpec {
	return []acceptanceSpec{
		halfAcceptance(R1, r1PresenceHalf, "R1_ok_version_present",
			[]acceptanceSubject{mappingKeySubject("version.presence", "$", "version")},
			[]acceptanceObservation{
				positionedKeyObservation("version.presence", "$", "version"),
			}),
		halfAcceptance(R1, r1ValueHalf, "R1_ok_version_one",
			[]acceptanceSubject{
				lexicalIntegerSubject("version.value", "$.version", "1", "01"),
			},
			[]acceptanceObservation{
				versionIntegerObservation("version.value", "$.version", "1", "1"),
			}),
		wholeAcceptance(R2, "R2_ok_instance_max_length",
			[]acceptanceSubject{
				scalarSubject("instance.max", "$.instance", strings.Repeat("a", 41),
					strings.Repeat("b", 40)),
			},
			[]acceptanceObservation{
				configObservation("instance.max", "the 41-rune instance boundary",
					func(c Config) bool { return c.Instance == strings.Repeat("a", 41) }),
			}),
		halfAcceptance(R3, r3PresenceHalf, "R3_ok_database_url_present",
			[]acceptanceSubject{
				mappingKeySubject("database.url.presence", "$.database", "url"),
			},
			[]acceptanceObservation{
				positionedKeyObservation("database.url.presence", "$.database", "url"),
			}),
		halfAcceptance(R3, r3ValueHalf, "R3_ok_connection_uri",
			[]acceptanceSubject{
				scalarSubject("database.url.postgresql", "$.database.url",
					"postgresql://noty@db.internal/noty", "postgres://noty@db.internal/noty"),
			},
			[]acceptanceObservation{
				configObservation("database.url.postgresql", "the postgresql URI",
					func(c Config) bool {
						return c.Database.URL == "postgresql://noty@db.internal/noty"
					}),
			}),
		wholeAcceptance(R4, "R4_ok_schema_63_bytes",
			[]acceptanceSubject{
				scalarSubject("database.schema.max", "$.database.schema",
					strings.Repeat("a", 63), strings.Repeat("b", 62)),
			},
			[]acceptanceObservation{
				configObservation("database.schema.max", "the 63-byte schema boundary",
					func(c Config) bool { return c.Database.Schema == strings.Repeat("a", 63) }),
			}),
		wholeAcceptance(R5, "R5_ok_connection_keyword_value",
			[]acceptanceSubject{
				scalarSubject("database.url.keyword", "$.database.url",
					"host=db.internal dbname=noty", "host=db2.internal dbname=noty"),
				scalarSubject("database.listen_url.keyword", "$.database.listen_url",
					"host=replica.internal dbname=noty", "host=replica2.internal dbname=noty"),
			},
			[]acceptanceObservation{
				configObservation("database.url.keyword", "the primary libpq string",
					func(c Config) bool { return c.Database.URL == "host=db.internal dbname=noty" }),
				configObservation("database.listen_url.keyword", "the listen libpq string",
					func(c Config) bool {
						return c.Database.ListenURL == "host=replica.internal dbname=noty"
					}),
			}),
		workerConcurrencyAcceptance(),
		workerBatchSizeAcceptance(),
	}
}

func acceptanceSpecsR1ToR10() []acceptanceSpec {
	return append(acceptanceSpecsR1ToR7(),
		workerDurationAcceptance(), leaseComparisonAcceptance(), retentionDurationAcceptance())
}

func workerConcurrencyAcceptance() acceptanceSpec {
	return wholeAcceptance(R6, "R6_ok_worker_concurrency_upper_bound",
		[]acceptanceSubject{
			scalarSubject("worker.concurrency.max", "$.worker.concurrency", "1024", "1023"),
		},
		[]acceptanceObservation{
			configObservation("worker.concurrency.max", "worker concurrency 1024",
				func(c Config) bool { return c.Worker.Concurrency == 1024 }),
		})
}

func workerBatchSizeAcceptance() acceptanceSpec {
	return wholeAcceptance(R7, "R7_ok_worker_batch_size_upper_bound",
		[]acceptanceSubject{
			scalarSubject("worker.batch_size.max", "$.worker.batch_size", "10000", "9999"),
		},
		[]acceptanceObservation{
			configObservation("worker.batch_size.max", "worker batch size 10000",
				func(c Config) bool { return c.Worker.BatchSize == 10000 }),
		})
}

func workerDurationAcceptance() acceptanceSpec {
	return wholeAcceptance(R8, "R8_ok_worker_durations_positive",
		[]acceptanceSubject{
			scalarSubject("worker.poll_interval", "$.worker.poll_interval", "500ms", "600ms"),
			scalarSubject("worker.lease_timeout", "$.worker.lease_timeout", "1s", "2s"),
			scalarSubject("worker.drain_timeout", "$.worker.drain_timeout", "1h", "2h"),
		},
		[]acceptanceObservation{
			configObservation("worker.poll_interval", "poll interval 500ms",
				func(c Config) bool { return c.Worker.PollInterval == 500*time.Millisecond }),
			configObservation("worker.lease_timeout", "lease timeout 1s",
				func(c Config) bool { return c.Worker.LeaseTimeout == time.Second }),
			configObservation("worker.drain_timeout", "drain timeout 1h",
				func(c Config) bool { return c.Worker.DrainTimeout == time.Hour }),
		})
}

func leaseComparisonAcceptance() acceptanceSpec {
	return wholeAcceptance(R9, "R9_ok_lease_timeout_strictly_above_listener_timeout",
		[]acceptanceSubject{
			scalarSubject("worker.lease_timeout", "$.worker.lease_timeout", "11s", "12s"),
			scalarSubject("listener.timeout", "$.listeners[0].timeout", "10s", "9s"),
		},
		[]acceptanceObservation{
			configObservation("worker.lease_timeout", "lease timeout 11s",
				func(c Config) bool { return c.Worker.LeaseTimeout == 11*time.Second }),
			configObservation("listener.timeout", "listener timeout 10s",
				func(c Config) bool {
					return len(c.Listeners) == 1 &&
						c.Listeners[0].Delivery.Timeout == 10*time.Second
				}),
		})
}

func retentionDurationAcceptance() acceptanceSpec {
	return wholeAcceptance(R10, "R10_ok_retention_durations_positive",
		[]acceptanceSubject{
			scalarSubject("retention.keep", "$.retention.keep", "168h", "169h"),
			scalarSubject("retention.partition_interval",
				"$.retention.partition_interval", "24h", "23h"),
			scalarSubject("retention.precreate", "$.retention.precreate", "24h", "25h"),
		},
		[]acceptanceObservation{
			configObservation("retention.keep", "retention keep 168h",
				func(c Config) bool { return c.Retention.Keep == 168*time.Hour }),
			configObservation("retention.partition_interval", "partition interval 24h",
				func(c Config) bool { return c.Retention.PartitionInterval == 24*time.Hour }),
			configObservation("retention.precreate", "precreate 24h",
				func(c Config) bool { return c.Retention.Precreate == 24*time.Hour }),
		})
}

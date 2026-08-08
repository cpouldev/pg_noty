package config

import (
	"log/slog"
	"slices"
	"strconv"
	"strings"
)

var _ slog.LogValuer = Config{}

// LogValue returns the resolved tree in contract order. Every string passes through the schema
// level that declares its path; the log surface owns no sensitivity list of its own.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("version", c.Version),
		slog.String("instance", schemaLogString(levelRoot, "instance", c.Instance)),
		logDatabase(c.Database),
		logWorker(c.Worker),
		logRetention(c.Retention),
		logListeners(c.Listeners),
	)
}

func logDatabase(database Database) slog.Attr {
	return slog.Group("database",
		slog.String("url", schemaLogString(levelDatabase, "url", database.URL)),
		slog.String("schema", schemaLogString(levelDatabase, "schema", database.Schema)),
		slog.String("listen_url", schemaLogString(levelDatabase, "listen_url", database.ListenURL)),
	)
}

func logWorker(worker Worker) slog.Attr {
	return slog.Group("worker",
		slog.Int("concurrency", worker.Concurrency),
		slog.Int("batch_size", worker.BatchSize),
		slog.Duration("poll_interval", worker.PollInterval),
		slog.Duration("lease_timeout", worker.LeaseTimeout),
		slog.Duration("drain_timeout", worker.DrainTimeout),
	)
}

func logRetention(retention Retention) slog.Attr {
	return slog.Group("retention",
		slog.Duration("keep", retention.Keep),
		slog.Duration("partition_interval", retention.PartitionInterval),
		slog.Duration("precreate", retention.Precreate),
	)
}

func logListeners(listeners []Listener) slog.Attr {
	attrs := make([]slog.Attr, 0, len(listeners))
	for index, listener := range listeners {
		attrs = append(attrs, indexedLogValue(index, logListener(listener)))
	}
	return slog.Attr{Key: "listeners", Value: slog.GroupValue(attrs...)}
}

func logListener(listener Listener) slog.Value {
	return slog.GroupValue(
		slog.String("name", schemaLogString(levelListener, "name", listener.Name)),
		slog.Bool("enabled", listener.Enabled),
		slog.Group("trigger",
			slog.String("table", schemaLogString(levelListener, "table", listener.Trigger.Table)),
			logOperations(listener.Trigger.Operations),
			logPayload(listener.Trigger.Payload),
		),
		slog.Group("delivery",
			logDestination(listener.Delivery.Destination),
			logRetry(listener.Delivery.Retry),
			slog.Duration("timeout", listener.Delivery.Timeout),
			slog.Int("concurrency", listener.Delivery.Concurrency),
		),
	)
}

func logOperations(operations Operations) slog.Attr {
	attrs := make([]slog.Attr, 0, len(operations))
	for index, operation := range operations {
		attrs = append(attrs, indexedLogValue(index, slog.GroupValue(
			slog.String("kind", operation.Kind),
			slog.Any("columns", operation.Columns),
			slog.Bool("is_distinct", operation.IsDistinct),
			slog.String("when", schemaLogString(levelOperation, "when", operation.When)),
		)))
	}
	return slog.Attr{Key: "operations", Value: slog.GroupValue(attrs...)}
}

func logPayload(payload Payload) slog.Attr {
	return slog.Group("payload",
		slog.String("mode", schemaLogString(levelPayload, "mode", payload.Mode)),
		slog.Any("columns", payload.Columns),
		slog.Any("exclude", payload.Exclude),
		slog.Bool("include_old", payload.IncludeOld),
		slog.Int("max_bytes", payload.MaxBytes),
	)
}

func logDestination(destination Destination) slog.Attr {
	return slog.Group("destination",
		slog.String("url", schemaLogString(levelDestination, "url", destination.URL)),
		slog.String("method", schemaLogString(levelDestination, "method", destination.Method)),
		logHeaders(destination.Headers),
		slog.Group("signing", logSecrets(destination.Signing.Secrets)),
	)
}

func logRetry(retry Retry) slog.Attr {
	return slog.Group("retry",
		slog.Int("max_attempts", retry.MaxAttempts),
		slog.String("backoff", schemaLogString(levelRetry, "backoff", retry.Backoff)),
		slog.Duration("initial_interval", retry.InitialInterval),
		slog.Duration("max_interval", retry.MaxInterval),
		slog.Bool("jitter", retry.Jitter),
	)
}

// logHeaders records which headers a destination carries, never what they hold. A destination
// header is where an operator puts the credential their endpoint expects -- Authorization, an API
// key, a shared token -- and this package's whole redaction contract is that a secret does not
// reach an output stream. The names are kept because "which headers are configured" is the
// question a reader of this log is asking; the values are not that question's answer.
//
// The value cannot be routed through schemaLogString the way every other string here is:
// levelHeaders is a free-form level with no declared keys, so the schema has nothing to say about
// which of them are sensitive, and asking it would redact the names too. Redacting unconditionally
// is the fail-closed reading, and it costs a reader nothing they were entitled to.
// TestHeaderValuesAreNeverLogged holds it.
func logHeaders(headers Headers) slog.Attr {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	slices.Sort(names)
	attrs := make([]slog.Attr, 0, len(names))
	for _, name := range names {
		attrs = append(attrs, slog.String(name, redactionPlaceholder))
	}
	return slog.Attr{Key: "headers", Value: slog.GroupValue(attrs...)}
}

func logSecrets(secrets []string) slog.Attr {
	attrs := make([]slog.Attr, 0, len(secrets))
	for index, secret := range secrets {
		attrs = append(attrs, slog.String(strconv.Itoa(index),
			schemaLogString(levelSigning, "secrets", secret)))
	}
	return slog.Attr{Key: "secrets", Value: slog.GroupValue(attrs...)}
}

func indexedLogValue(index int, value slog.Value) slog.Attr {
	return slog.Attr{Key: strconv.Itoa(index), Value: value}
}

// schemaLogString derives containment from schema.go. Unknown paths and unknown sensitivity kinds
// fail closed. A sensitive value containing a physical line break is hidden wholesale so no tail
// sharing that line can be reintroduced after a partial URL-password replacement.
func schemaLogString(level levelName, key, value string) string {
	spec, declared := schemaLevels[level].key(key)
	if !declared {
		return redactionPlaceholder
	}
	switch spec.logSensitivity {
	case publicValue:
		return value
	case urlPassword:
		if strings.ContainsAny(value, "\r\n") {
			return redactionPlaceholder
		}
		return redactedConnectionString(value)
	case entireValue:
		return redactionPlaceholder
	default:
		return redactionPlaceholder
	}
}

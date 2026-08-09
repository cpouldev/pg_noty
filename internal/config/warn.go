package config

const (
	literalSecretWarning = "destination.signing.secrets should use an interpolated environment " +
		"reference instead of a YAML literal; move the secret to the environment and write ${VARIABLE_NAME}"
	drainTimeoutWarning = "worker.drain_timeout is smaller than the largest effective " +
		"listeners[].timeout; increase the drain timeout or lower the listener timeout so shutdown " +
		"does not abandon in-flight requests"
)

// warningsFor is the closed W1/W2 warning layer. It runs only after stage D has succeeded, so an
// interpolation stop returns no warning, while later errors can be returned beside these findings.
func warningsFor(raw *rawConfig, cfg *Config, originals originalTexts) Warnings {
	warnings := literalSecretWarnings(raw, originals)
	if warning, raised := drainTimeoutTooSmall(raw, cfg); raised {
		warnings = append(warnings, warning)
	}
	return warnings
}

func literalSecretWarnings(raw *rawConfig, originals originalTexts) Warnings {
	var warnings Warnings
	for i := range raw.Listeners.Values {
		secrets := raw.Listeners.Values[i].Destination.Value.Signing.Value.Secrets.values
		for _, secret := range secrets {
			if _, suppliedByEnvironment := originals[secret.node]; suppliedByEnvironment {
				continue
			}
			warnings = append(warnings, NewWarning(W1, secret, literalSecretWarning))
		}
	}
	return warnings
}

func drainTimeoutTooSmall(raw *rawConfig, cfg *Config) (Warning, bool) {
	drain := resolvedDuration(cfg.Worker.DrainTimeout, raw.Worker.Value.DrainTimeout)
	timeout, found := largestListenerTimeout(raw, cfg)
	if !found || !drain.positive() || drain.value >= timeout.value {
		return Warning{}, false
	}
	at, anchored := comparisonAnchor(drain.source, timeout.source)
	if !anchored {
		return Warning{}, false
	}
	return NewWarning(W2, at, drainTimeoutWarning), true
}

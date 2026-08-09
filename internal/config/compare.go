package config

import "time"

var (
	leaseComparisonFault = semanticFault(
		"worker.lease_timeout must be greater than the largest effective listeners[].timeout; " +
			"increase the lease or lower the timeout because a lease no longer than a request timeout " +
			"causes duplicate deliveries")
	keepComparisonFault = semanticFault(
		"retention.keep must be at least retention.partition_interval; " +
			"increase retention.keep or lower retention.partition_interval")
	precreateComparisonFault = semanticFault(
		"retention.precreate must be at least retention.partition_interval; " +
			"increase retention.precreate or lower retention.partition_interval")
	retryPositiveFault   = semanticFault("retry.initial_interval must be greater than zero")
	retryComparisonFault = semanticFault(
		"retry.initial_interval must be no greater than retry.max_interval; " +
			"lower retry.initial_interval or raise retry.max_interval")
	concurrencyComparisonFault = semanticFault(
		"listeners[].concurrency must be no greater than worker.concurrency; " +
			"lower the listener concurrency or raise worker.concurrency")
)

type effectiveDuration struct {
	value  time.Duration
	source Dur
}

func resolvedDuration(value time.Duration, written ...Dur) effectiveDuration {
	for _, source := range written {
		if source.Set {
			return effectiveDuration{value: value, source: source}
		}
	}
	return effectiveDuration{value: value}
}

func (e effectiveDuration) readable() bool {
	return !e.source.Set || e.source.Valid()
}

func (e effectiveDuration) positive() bool {
	return e.readable() && e.value > 0
}

type effectiveInt struct {
	value  int
	source Int
}

type writtenPositioned interface {
	Positioned
	wasWritten() bool
}

func resolvedInt(value int, written ...Int) effectiveInt {
	for _, source := range written {
		if source.Set {
			return effectiveInt{value: value, source: source}
		}
	}
	return effectiveInt{value: value}
}

func (e effectiveInt) comparableConcurrency() bool {
	return (!e.source.Set || e.source.Valid()) &&
		concurrencyCanParticipateInComparison(e.value)
}

// comparisonAnchor is D2 for every effective rule: the first-named written operand wins, then the
// other written operand. Internally consistent built-ins mean a broken comparison always has one.
func comparisonAnchor(left, right writtenPositioned) (Positioned, bool) {
	if left.wasWritten() {
		return left, true
	}
	if right.wasWritten() {
		return right, true
	}
	return nil, false
}

func (v *validatePass) validateLeaseComparison(rule RuleID, raw *rawConfig, cfg *Config) {
	lease := resolvedDuration(cfg.Worker.LeaseTimeout, raw.Worker.Value.LeaseTimeout)
	timeout, found := largestListenerTimeout(raw, cfg)
	if !found || !lease.positive() || lease.value > timeout.value {
		return
	}
	if at, anchored := comparisonAnchor(lease.source, timeout.source); anchored {
		v.reportEffective(rule, at, leaseComparisonFault)
	}
}

func (v *validatePass) validateRetentionComparisons(
	keepRule, precreateRule RuleID, raw *rawConfig, cfg *Config,
) {
	partition := resolvedDuration(cfg.Retention.PartitionInterval,
		raw.Retention.Value.PartitionInterval)
	keep := resolvedDuration(cfg.Retention.Keep, raw.Retention.Value.Keep)
	precreate := resolvedDuration(cfg.Retention.Precreate, raw.Retention.Value.Precreate)
	v.validateAtLeast(keepRule, keep, partition, keepComparisonFault)
	v.validateAtLeast(precreateRule, precreate, partition, precreateComparisonFault)
}

func (v *validatePass) validateAtLeast(
	rule RuleID, left, right effectiveDuration, why fault,
) {
	if !left.positive() || !right.positive() || left.value >= right.value {
		return
	}
	if at, anchored := comparisonAnchor(left.source, right.source); anchored {
		v.reportEffective(rule, at, why)
	}
}

func (v *validatePass) validateRetryComparison(
	rule RuleID, defaults, listener rawRetry, retry Retry,
) {
	initial := resolvedDuration(retry.InitialInterval,
		listener.InitialInterval, defaults.InitialInterval)
	if !initial.readable() {
		return
	}
	if initial.value <= 0 {
		if initial.source.Set {
			v.reportEffective(rule, initial.source, retryPositiveFault)
		}
		return
	}
	maximum := resolvedDuration(retry.MaxInterval, listener.MaxInterval, defaults.MaxInterval)
	if !maximum.positive() || initial.value <= maximum.value {
		return
	}
	if at, anchored := comparisonAnchor(initial.source, maximum.source); anchored {
		v.reportEffective(rule, at, retryComparisonFault)
	}
}

func (v *validatePass) validateConcurrencyComparison(
	rule RuleID, rawWorker, rawListener Int, worker, listener int,
) {
	left := resolvedInt(listener, rawListener)
	right := resolvedInt(worker, rawWorker)
	if !left.comparableConcurrency() || !right.comparableConcurrency() ||
		left.value <= right.value {
		return
	}
	if at, anchored := comparisonAnchor(left.source, right.source); anchored {
		v.reportEffective(rule, at, concurrencyComparisonFault)
	}
}

func largestListenerTimeout(raw *rawConfig, cfg *Config) (effectiveDuration, bool) {
	var largest effectiveDuration
	found := false
	for i := range raw.Listeners.Values {
		candidate := resolvedDuration(cfg.Listeners[i].Delivery.Timeout,
			raw.Listeners.Values[i].Timeout, raw.Defaults.Value.Timeout)
		if !candidate.positive() {
			continue
		}
		if !found || candidate.value > largest.value {
			largest, found = candidate, true
		}
	}
	return largest, found
}

func (v *validatePass) reportEffective(rule RuleID, at Positioned, why fault) {
	reportSemanticUsing(&v.stage, rule, effectiveValueUse, anchorSet{value: at}, why)
}

package delivery

import "time"

// JitterFunc supplies one uniformly distributed integer in [0, max). DelayWith
// passes ladder+1 for an inclusive ladder endpoint (or MaxInt64 when that count
// cannot be represented). The worker injects it; nil leaves the ladder stable.
type JitterFunc func(max int64) int64

// Policy is the resolved listener retry policy. Its methods are pure: the only
// non-policy input is the explicit jitter draw supplied to DelayWith.
type Policy struct {
	MaxAttempts     int
	Backoff         string
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Jitter          bool
}

// Ladder returns the deterministic, max-clamped delay for an attempt. Attempts
// are one-based; an out-of-range lower value is treated as the first attempt.
func (p Policy) Ladder(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := positiveDuration(p.InitialInterval)
	var delay time.Duration
	switch p.Backoff {
	case "linear":
		delay = multiplyDuration(base, int64(attempt))
	case "exponential":
		delay = multiplyDuration(base, powerOfTwo(attempt-1))
	default:
		// Config validation admits only the three named modes. A defensive
		// policy value falls back to fixed rather than becoming a hot loop.
		delay = base
	}
	return clampDuration(delay, p.MaxInterval)
}

// DelayWith applies Full Jitter to the clamped ladder. The upper bound is
// inclusive for every representable bound; at MaxInt64, ladder+1 cannot be
// represented, so the draw range is [0, MaxInt64) and remains overflow-safe.
func (p Policy) DelayWith(attempt int, draw JitterFunc) time.Duration {
	limit := p.Ladder(attempt)
	if !p.Jitter || draw == nil || limit <= 0 {
		return limit
	}
	n := int64(limit)
	bound := n
	if n < maxDurationInt64 {
		bound++
	}
	value := draw(bound)
	if value < 0 {
		value = 0
	}
	if value > n {
		value = n
	}
	return time.Duration(value)
}

// EffectiveDelay applies a Retry-After floor and then the policy cap. Invalid
// negative inputs are treated as zero; equality at maxInterval is preserved.
func EffectiveDelay(ladder, retryAfter, maxInterval time.Duration) time.Duration {
	delay := positiveDuration(ladder)
	if retryAfter > delay {
		delay = retryAfter
	}
	return clampDuration(delay, maxInterval)
}

// DelayAfter combines the pure ladder, optional jitter and a parsed
// Retry-After delay. The caller supplies parsed retryAfter rather than a clock.
func (p Policy) DelayAfter(attempt int, retryAfter time.Duration, draw JitterFunc) time.Duration {
	return EffectiveDelay(p.DelayWith(attempt, draw), retryAfter, p.MaxInterval)
}

// Terminal reports whether an outcome at attempt has exhausted the configured
// number of attempts. Zero or negative limits fail closed as terminal.
func (p Policy) Terminal(attempt int) bool {
	return p.MaxAttempts <= 0 || attempt >= p.MaxAttempts
}

// ShouldRetry combines status classification with the max-attempt boundary.
func (p Policy) ShouldRetry(attempt int, outcome Outcome) bool {
	return outcome == OutcomeRetryable && !p.Terminal(attempt)
}

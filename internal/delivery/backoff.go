package delivery

import (
	"math/rand"
	"time"
)

const maxDurationInt64 = int64(^uint64(0) >> 1)

func randomJitter(max int64) int64 {
	if max <= 1 {
		return 0
	}
	return rand.Int63n(max)
}

func positiveDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}

func clampDuration(value, maximum time.Duration) time.Duration {
	if maximum > 0 && value > maximum {
		return maximum
	}
	return value
}

func multiplyDuration(value time.Duration, factor int64) time.Duration {
	if value <= 0 || factor <= 0 {
		return 0
	}
	if factor > maxDurationInt64/int64(value) {
		return time.Duration(maxDurationInt64)
	}
	return value * time.Duration(factor)
}

func powerOfTwo(exponent int) int64 {
	if exponent <= 0 {
		return 1
	}
	if exponent >= 63 {
		return maxDurationInt64
	}
	if exponent == 62 {
		return int64(1 << 62)
	}
	return int64(1 << exponent)
}

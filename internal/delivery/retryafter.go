package delivery

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ParseRetryAfter parses RFC 9110's delta-seconds and HTTP-date forms. The
// reference clock is injected so past/future decisions do not consult global
// wall-clock state. Malformed, negative and past values are absent.
func ParseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if delay, ok := parseDeltaSeconds(value); ok {
		return delay, true
	}
	when, ok := parseHTTPDate(value)
	if !ok {
		return 0, false
	}
	delay := when.Sub(now)
	if delay <= 0 {
		return 0, false
	}
	return delay, true
}

func parseDeltaSeconds(value string) (time.Duration, bool) {
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	seconds, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		// RFC 9110 permits an arbitrarily long non-negative decimal. Saturate
		// rather than dropping a valid floor; EffectiveDelay will cap it.
		return time.Duration(1<<63 - 1), true
	}
	const maxDuration = uint64(1<<63 - 1)
	if seconds > maxDuration/uint64(time.Second) {
		return time.Duration(1<<63 - 1), true
	}
	return time.Duration(seconds) * time.Second, true
}

func parseHTTPDate(value string) (time.Time, bool) {
	when, err := http.ParseTime(value)
	return when, err == nil
}

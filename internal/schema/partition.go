package schema

import (
	"slices"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
)

// Range is one partition's extent and the name of the table realising it. The extent is half-open:
// FROM is inclusive and TO is exclusive (M11), so an event occurring at exactly To belongs to the
// next range and not to this one. Every comparison in this file follows that rule, and each of them
// has its equality case as a named row in this package's corpus.
type Range struct {
	From time.Time
	To   time.Time
	// Name is the unqualified partition name, derived from the extent by rangeName.
	Name string
}

const (
	// rangeBoundLayout writes one bound at fixed width and full precision. Fixed width matters because
	// two stamps then differ whenever their instants do; nanosecond precision matters because
	// internal/config requires only that partition_interval be greater than zero (R10), and at a
	// sub-second interval a second-resolution stamp would name several ranges alike.
	rangeBoundLayout = "20060102T150405.000000000"
)

// RequiredRanges is every partition that must exist for a write to succeed now and for as far ahead
// as the pre-creation horizon reaches: k from floor(now/interval) through
// floor((now+precreate)/interval), inclusive at both ends (ADR-6).
//
// The horizon end is inclusive because bounds are half-open (M11): an event written at exactly
// now+precreate belongs to the range that *starts* there, so that range has to exist. The count is
// a consequence of that k-range rather than a formula -- internal/config guarantees precreate >=
// partition_interval (R12) but says nothing about divisibility, so the count moves by one as now
// advances inside one interval.
//
// cfg.PartitionInterval must be greater than zero, which internal/config's R10 guarantees. A
// non-positive one divides by zero and panics here, deliberately: the standard library does the
// same for a non-positive tick, and the alternative -- returning a set covering nothing -- would be
// a wrong answer rather than a loud one.
func RequiredRanges(now time.Time, cfg config.Retention) []Range {
	first := gridIndex(now, cfg.PartitionInterval)
	last := gridIndex(now.Add(cfg.Precreate), cfg.PartitionInterval)

	required := make([]Range, 0, int(last-first+1))
	for index := first; index <= last; index++ {
		required = append(required, rangeAt(index, cfg.PartitionInterval))
	}
	return required
}

// ExpiredRanges is every observed range holding nothing that must still be kept: one whose upper
// bound is at or before now-keep. TO is exclusive (M11), so a range ending exactly at the cutoff
// holds no event at or after it and is expired, while the range one interval later holds the cutoff
// instant itself and is not. Observed order is preserved.
func ExpiredRanges(observed []Range, now time.Time, keep time.Duration) []Range {
	cutoff := now.Add(-keep)

	var expired []Range
	for _, ranged := range observed {
		if !ranged.To.After(cutoff) {
			expired = append(expired, ranged)
		}
	}
	return expired
}

// CoverageShortfall is how far short of the horizon -- now + precreate -- the observed partitions
// reach, counting only coverage contiguous from now: a hole ends the reach, because a write landing
// in it fails however far the partitions beyond it extend. It is zero when the coverage reaches the
// horizon exactly, and zero beyond it rather than negative.
func CoverageShortfall(observed []Range, now time.Time, precreate time.Duration) time.Duration {
	reach := now
	for _, ranged := range sortedByStart(observed) {
		if ranged.From.After(reach) {
			break
		}
		if ranged.To.After(reach) {
			reach = ranged.To
		}
	}

	horizon := now.Add(precreate)
	if !reach.Before(horizon) {
		return 0
	}
	return horizon.Sub(reach)
}

// sortedByStart is the observed set in ascending order of lower bound: the catalog hands partitions
// over in whatever order it read them. The input is cloned rather than sorted in place.
func sortedByStart(observed []Range) []Range {
	ordered := slices.Clone(observed)
	slices.SortFunc(ordered, func(a, b Range) int { return a.From.Compare(b.From) })
	return ordered
}

// gridIndex is k such that boundary(k) <= instant < boundary(k+1): how many whole intervals separate
// 1970-01-01T00:00:00Z from the instant, counted with a true floor (ADR-6).
//
// The arithmetic is nanosecond-based, so it is valid over the range an int64 of nanoseconds from the
// epoch expresses -- 1678-09-21 to 2262-04-11. One grid line past that limit wraps rather than
// erroring, and TestTheGridIsNanosecondBasedAndThereforeStopsIn2262 pins both the limit and the
// wrap, so a later switch to a seconds-based representation fails there instead of silently widening
// what this arithmetic claims to serve.
func gridIndex(instant time.Time, interval time.Duration) int64 {
	return floorDiv(instant.UnixNano(), int64(interval))
}

// floorDiv divides rounding toward negative infinity, which is exactly what Go's `/` does not do: it
// truncates toward zero, so -1 / 86400e9 is 0 where the floor is -1. A pre-1970 instant would
// otherwise map to the range *after* the one holding it, and no post-epoch input can tell the two
// implementations apart. The remainder is tested as well as the sign, because an exact negative
// multiple is already floored and decrementing it would answer one range too low.
//
// The divisor is always a partition interval, which internal/config validates greater than zero
// (R10), so only the numerator's sign is considered. This is the package's one flooring helper:
// gridIndex is its only caller, and every boundary here comes from gridIndex, so no second copy can
// drift.
func floorDiv(numerator, divisor int64) int64 {
	quotient := numerator / divisor
	if numerator%divisor != 0 && numerator < 0 {
		return quotient - 1
	}
	return quotient
}

// boundary is the grid line at index k: k whole intervals from 1970-01-01T00:00:00Z, in UTC. No
// boundary is ever derived from now, which is what makes two boots inside one interval agree about
// which partitions exist -- and the server refuses overlapping ranges, so disagreeing is fatal to
// the second boot (ADR-6).
func boundary(index int64, interval time.Duration) time.Time {
	return time.Unix(0, 0).UTC().Add(time.Duration(index) * interval)
}

// rangeAt is the range occupying one grid index, named.
func rangeAt(index int64, interval time.Duration) Range {
	from, to := boundary(index, interval), boundary(index+1, interval)
	return Range{From: from, To: to, Name: rangeName(from, to)}
}

// rangeName is the partition name for one range of TableEvents. Both bounds are encoded rather than
// only the lower one: partition_interval is configuration and can change between boots, so [T, T+1h)
// and [T, T+24h) are distinct ranges one catalog may be asked for in its lifetime, and a name
// derived from the start alone would collide them and leave the second permanently uncreatable.
//
// Both bounds are written in UTC, so the name does not depend on the zone of the process that
// computed it -- a replica elsewhere has to agree, because `CREATE TABLE IF NOT EXISTS ... PARTITION
// OF` matches on the name (M6).
func rangeName(from, to time.Time) string {
	return ObjectName(TableEvents, boundStamp(from)+"_"+boundStamp(to))
}

// boundStamp writes one bound of a range as the absolute instant it is.
func boundStamp(bound time.Time) string {
	return bound.UTC().Format(rangeBoundLayout) + "Z"
}

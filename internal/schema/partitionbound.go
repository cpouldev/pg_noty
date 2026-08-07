package schema

import (
	"fmt"
	"strings"
	"time"
)

// This file reads what pg_get_expr(relpartbound, oid) writes, and nothing else: pure text and
// arithmetic, so every class it recognises and every class it refuses has a container-free row
// taken from recorded server output. M12 rests on it -- a bound read back here is what makes the
// bound assertions in Steps 11, 13, 14 and 16 mechanical rather than inferred from behaviour.
//
// The refusals carry more weight than the readings. A partition whose bound cannot be read is
// refused rather than skipped, because the observation catalog.go builds out of this is the world
// Step 14 computes a drop against, and a partition missing from it is one retention decided
// without ever seeing. The refusal is unconditional: nothing the bound string supplies switches it
// off, and the shapes it exists to catch are exactly the ones nobody measured.
//
// One omission from that observation is not this refusal being softened, and the difference is worth
// keeping straight: a partition dropped by another replica mid-query renders no bound at all, and
// cataloginventory.go skips it before this reader is ever asked. Nothing here is reached with a
// bound that was not written, so every string that arrives is one the server rendered and every
// refusal below still stands.
//
// MAXVALUE is why the answer is "refused" rather than "reported with a zero end". plan.go's
// whyUndroppable exempts any partition the catalog reports without an upper bound, so a
// MAXVALUE-bounded partition handed over with a zero To would be exempt from retention forever,
// silently, and would hold expired data for as long as it existed.

// defaultBoundExpression is what the server renders for a DEFAULT partition. Measured on PostgreSQL
// 17.10 and pinned against the running server by
// TestTheServerStillRendersEveryShapeTheReaderWasWrittenFor.
const defaultBoundExpression = "DEFAULT"

// The three pieces of a single-column range bound as the server writes it. They are cut off the
// string rather than matched as a pattern, so a two-column bound -- FROM ('...', 0) TO ('...', 0)
// -- fails to close and a MAXVALUE bound fails to open, instead of either parsing halfway.
const (
	rangeBoundOpening = "FOR VALUES FROM ('"
	rangeBoundMiddle  = "') TO ('"
	rangeBoundClosing = "')"
)

// The two spellings a range unbounded on one side takes. They are consulted only to name why a
// bound was refused; the refusal itself has already happened by the time either is read, so neither
// is a condition on the guard.
const (
	unboundedBelow = "FOR VALUES FROM (MINVALUE)"
	unboundedAbove = "TO (MAXVALUE)"
)

// isoBoundLayouts are the three offset widths a timestamptz takes under DateStyle ISO: whole hours
// (+00), hours and minutes (+05:30), and hours, minutes and seconds (+00:17:30, which a local mean
// time zone produces for a pre-1900 instant). All three were measured; the fraction is optional
// because the server writes one only when the instant carries microseconds.
//
// DateStyle itself is the dependency, and it is the whole dependency: under ISO the offset is
// carried, so any TimeZone renders an instant this reader recovers exactly. Under any other
// DateStyle the rendering is refused rather than guessed at, and
// TestObservationRefusesABoundRenderedUnderAnotherDateStyle reaches that refusal against
// the running server.
var isoBoundLayouts = []string{
	"2006-01-02 15:04:05.999999-07",
	"2006-01-02 15:04:05.999999-07:00",
	"2006-01-02 15:04:05.999999-07:00:00",
}

// boundFault is why a partition's bound could not be read as a Range. Each reason has its own
// answer rather than sharing one negative return, so a caller can say what is actually wrong.
type boundFault string

const (
	boundOK boundFault = "ok"
	// boundNotASingleColumnRange covers every bound that is not this parent's shape at all: a list
	// or hash partition, a range over more than one column, and the DEFAULT partition, which
	// isDefaultBound answers before this reader is ever asked.
	boundNotASingleColumnRange boundFault = "is not a single-column range bound of the form " +
		"FOR VALUES FROM (...) TO (...)"
	// boundHasNoFiniteEnd is the one plan.go cannot be told about. A Range carries two instants and
	// has no way to spell an unbounded end, and a zero one would read as an age older than every
	// retention cutoff.
	boundHasNoFiniteEnd boundFault = "names MINVALUE or MAXVALUE, and a Range has no way to " +
		"carry an unbounded end"
	// boundInstantUnreadable is the shape matching and the ends not being instants this reader
	// recovers -- a column carrying no zone, or a DateStyle other than ISO.
	boundInstantUnreadable boundFault = "does not write both ends as an ISO timestamp carrying a " +
		"zone, which is what a DateStyle other than ISO renders"
)

// boundFaults is the vocabulary as a value, so a test quantifies over it rather than over a list
// copied into one assertion.
var boundFaults = []boundFault{
	boundOK, boundNotASingleColumnRange, boundHasNoFiniteEnd, boundInstantUnreadable,
}

// isDefaultBound reports whether a bound expression is the DEFAULT partition's.
//
// It is asked before rangeFrom and never instead of it: the DEFAULT partition is tagged by the
// observation and never carried as a Range, because a comparable upper bound on a partition that
// has none is exactly what would make retention treat it as infinitely old (criterion 36).
func isDefaultBound(written string) bool {
	return written == defaultBoundExpression
}

// rangeFrom is the extent one bounded partition covers, or the reason its bound could not be read.
func rangeFrom(written, name string) (Range, boundFault) {
	from, to, isRange := rangeBoundLiterals(written)
	if !isRange {
		return Range{}, whyNotASingleColumnRange(written)
	}

	begins, readable := instantAt(from)
	if !readable {
		return Range{}, boundInstantUnreadable
	}
	ends, readable := instantAt(to)
	if !readable {
		return Range{}, boundInstantUnreadable
	}
	return Range{From: begins, To: ends, Name: name}, boundOK
}

// rangeBoundLiterals is the two quoted texts a single-column range bound holds, undecoded.
//
// A literal carrying a doubled quote of its own is left as it is rather than unescaped, because no
// instant spells one: such a bound belongs to a text-keyed parent and fails to read as an instant a
// moment later, which is the answer it should get.
func rangeBoundLiterals(written string) (from, to string, isRange bool) {
	opened, opens := strings.CutPrefix(written, rangeBoundOpening)
	if !opens {
		return "", "", false
	}
	closed, closes := strings.CutSuffix(opened, rangeBoundClosing)
	if !closes {
		return "", "", false
	}
	return strings.Cut(closed, rangeBoundMiddle)
}

// whyNotASingleColumnRange names which of the two non-range shapes was written. It refines a
// refusal that has already been decided; the unbounded spellings are worth their own reason because
// they are the one class a caller would otherwise have to guess at from a generic message.
func whyNotASingleColumnRange(written string) boundFault {
	if strings.HasPrefix(written, unboundedBelow) || strings.HasSuffix(written, unboundedAbove) {
		return boundHasNoFiniteEnd
	}
	return boundNotASingleColumnRange
}

// instantAt reads one bound literal as the instant it names, trying each offset width the server
// writes. time.Parse consumes the whole string or fails, so a literal carrying anything past the
// offset -- a second column, a trailing zone name -- is refused rather than read partially.
func instantAt(written string) (time.Time, bool) {
	for _, layout := range isoBoundLayouts {
		if instant, err := time.Parse(layout, written); err == nil {
			return instant, true
		}
	}
	return time.Time{}, false
}

// unreadableBound refuses one partition whose bound could not be read, naming the consequence as
// well as the reason: an observation missing a partition is one a drop decision was computed
// without, and this message is what a later reader meets before turning the refusal into a skip.
func unreadableBound(name, written string, fault boundFault) error {
	return fmt.Errorf("the bound of partition %s %s (%s); reporting the other partitions without "+
		"it would leave retention deciding against an incomplete world", name, fault, written)
}

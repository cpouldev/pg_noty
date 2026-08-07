package schema

import (
	"slices"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
)

// Plan is everything one maintenance pass would change: the partitions that must be created for
// writes to keep succeeding, and the ones whose whole extent has aged out of the retention window.
//
// Both slices hold Range values carrying From, To and Name together rather than names alone. That is
// what lets Step 11 reconcile a creation race by comparing the *observed bound*: CREATE TABLE IF NOT
// EXISTS ... PARTITION OF matches on the name, so two replicas that agreed on a name but disagreed
// on an extent would be told the relation already exists and would carry on with the wrong one (M6).
//
// Drop never contains the DEFAULT partition -- see whyUndroppable.
type Plan struct {
	Create []Range
	Drop   []Range
}

// PlanMaintenance is the whole decision surface of a maintenance pass, as a pure function of the
// instant, the configuration and what the catalog reports. internal/cli's --dry-run is this call
// and nothing else, which is why it is exported.
//
// It is total: the same arguments give the same plan on any machine, in any process timezone, and at
// any instant inside one interval, because every boundary comes from the epoch grid and never from
// now (ADR-6). Criterion 22's idempotency is a consequence rather than a guard -- a pass over a
// converged catalog plans nothing because there is nothing to plan, not because an `if` skipped it.
//
// It still does not police how many ranges the configuration asks for, and it has nowhere to: its
// only expressible refusal is an empty Plan, which already means "already converged". The refusal is
// horizon.go's unservableHorizonIn, and the two entry points that own an error channel -- Bootstrap
// and Maintain -- ask it before ever reaching this function.
func PlanMaintenance(now time.Time, cfg config.Config, observed []Range) Plan {
	return Plan{
		Create: rangesNotYetObserved(RequiredRanges(now, cfg.Retention), observed),
		Drop:   ExpiredRanges(retainedByAge(observed), now, cfg.Retention.Keep),
	}
}

// rangesNotYetObserved is every required range no observed partition already covers, in required
// order.
//
// Membership is decided by extent and never by name. A replica that lost a creation race left a
// partition under a name this process would also have chosen, but a partition created under an
// earlier configuration, or by a DBA, covers the same extent under a different one -- and asking for
// it again is what the server answers with `would overlap partition ...` (AC 23).
func rangesNotYetObserved(required, observed []Range) []Range {
	var missing []Range
	for _, wanted := range required {
		if !slices.ContainsFunc(observed, wanted.sameExtentAs) {
			missing = append(missing, wanted)
		}
	}
	return missing
}

// sameExtentAs reports whether two ranges cover the same half-open extent. The instants are compared
// with Equal rather than ==, because == also compares the monotonic reading and the location, and
// two records of one instant from different sources need agree on neither.
func (ranged Range) sameExtentAs(other Range) bool {
	return ranged.From.Equal(other.From) && ranged.To.Equal(other.To)
}

// undroppableReason is why one observed partition is exempt from retention. It is a reason rather
// than a bool so that a caller -- and the corpus -- can say which rule protected a partition, since
// two rules protect the DEFAULT one and only one of them protects anything else.
type undroppableReason string

const (
	// droppable is the absence of a reason. It is the empty string, which no reason below spells,
	// so it can never be confused with one.
	droppable undroppableReason = ""
	// itIsTheDefaultPartition protects the partition every unrouted row lands in. Dropping it makes
	// a write for any uncovered range fail outright instead of landing somewhere recoverable, and it
	// is never recreated by a maintenance pass because migration 2 owns it (criterion 36).
	itIsTheDefaultPartition undroppableReason = "it is the DEFAULT partition"
	// itHasNoUpperBound protects anything the catalog reports without one. Criterion 36's own
	// reasoning is that a partition with no upper bound has no age, and an age rule reading a zero
	// instant as year 1 concludes the opposite -- that it is older than every cutoff.
	itHasNoUpperBound undroppableReason = "it has no upper bound, and therefore no age"
)

// whyUndroppable is the retention exemption, and it answers with the *first* rule that applies. The
// order matters for the DEFAULT partition, which violates both: naming the missing bound would send
// a reader looking for a bounds defect instead of telling them the rule that protected it. The row
// that pins the order is the one in theUndroppableRows carrying neither bound.
func whyUndroppable(observed Range) undroppableReason {
	if observed.Name == PartitionDefault {
		return itIsTheDefaultPartition
	}
	if observed.To.IsZero() {
		return itHasNoUpperBound
	}
	return droppable
}

// retainedByAge is the observed partitions retention may judge at all, in observed order.
//
// The exemption is applied before the age rule rather than to its result, because the exempt
// partitions are exactly the ones the age rule cannot read: it compares an upper bound against the
// cutoff, and these have none to compare. Both orders answer identically today, so this is a reason
// and not a claim about precedence.
func retainedByAge(observed []Range) []Range {
	var judgeable []Range
	for _, candidate := range observed {
		if whyUndroppable(candidate) == droppable {
			judgeable = append(judgeable, candidate)
		}
	}
	return judgeable
}

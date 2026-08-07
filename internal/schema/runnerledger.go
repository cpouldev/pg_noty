package schema

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// This file is ADR-5's ledger: what a database records as applied, and the one reason a recorded
// ledger cannot be carried forward by a corpus. runner.go is the other half -- what is applied, and
// how.
//
// The ledger is a row per applied version and its primary key is the concurrency mechanism, not
// bookkeeping. A second concurrent application of version k does not leave a second row to be
// noticed later: it raises a unique violation inside that migration's own transaction and rolls the
// migration back. That is also why criterion 16 compares the recorded *set* against 1..N rather
// than its maximum -- a maximum cannot tell {1,2,3} from {1,2,4} or from {1,2,3,3}.

// The runner's own statements. Each writes the ledger's name unqualified and resolves it through the
// search_path its transaction has just set, so the schema is interpolated once (D1) rather than
// four times. to_regclass answers NULL rather than raising for a name nothing declares, which is
// what makes an absent ledger an empty applied set instead of an error on every first boot.
const (
	ledgerPresence = "SELECT to_regclass('" + TableSchemaVersion + "') IS NOT NULL"
	ledgerRows     = "SELECT version, checksum FROM " + TableSchemaVersion + " ORDER BY version"
	ledgerInsert   = "INSERT INTO " + TableSchemaVersion +
		" (version, checksum, applied_at) VALUES ($1, $2, now())"
)

// appliedVersion is one row of the ledger.
type appliedVersion struct {
	version  int
	checksum string
}

// ledgerVerdict is the one reason a recorded ledger cannot be carried forward by a corpus, or
// ledgerUsable. Each reason has its own answer rather than sharing one "unusable ledger", so a
// caller can say what is actually wrong.
type ledgerVerdict string

const (
	ledgerUsable        ledgerVerdict = "usable"
	ledgerAheadOfBinary ledgerVerdict = "records a version this binary does not embed"
	ledgerNotAPrefix    ledgerVerdict = "does not account for every version below its highest"
	ledgerFileChanged   ledgerVerdict = "records a checksum the file of that version no longer has"
)

// readLedger is every version the database records as applied, ascending. An absent ledger is an
// empty set and not an error (ADR-10): the first boot has none, and treating that as a failure
// would break bootstrap entirely.
func readLedger(ctx context.Context, db txBeginner, setPath string) ([]appliedVersion, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, finished(err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, setPath); err != nil {
		return nil, finished(err)
	}
	var present bool
	if err := tx.QueryRow(ctx, ledgerPresence).Scan(&present); err != nil {
		return nil, finished(err)
	}
	if !present {
		return nil, nil
	}

	rows, err := tx.Query(ctx, ledgerRows)
	if err != nil {
		return nil, finished(err)
	}
	recorded, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (appliedVersion, error) {
		var found appliedVersion
		return found, row.Scan(&found.version, &found.checksum)
	})
	return recorded, finished(err)
}

// whyUnusableLedger is the one reason a recorded ledger cannot be carried forward, and the recorded
// row that reason fired on.
//
// The order is load-bearing rather than cosmetic, and each pair of reasons has a case violating both
// at once. Ahead first, because a binary that does not embed the database's version is the wrong
// binary and every later finding is a consequence of that. The prefix test next, because the
// checksum comparison reads corpus[i] for every recorded row and it is these two together that bound
// it: ahead gives max(recorded) <= max(corpus), the prefix gives max(recorded) == len(recorded), and
// versionGapIn gives max(corpus) == len(corpus).
func whyUnusableLedger(recorded []appliedVersion, corpus []migration) (ledgerVerdict, int) {
	if len(recorded) == 0 {
		return ledgerUsable, 0
	}
	if highestRecorded(recorded) > highestEmbedded(corpus) {
		return ledgerAheadOfBinary, len(recorded) - 1
	}

	for i, row := range recorded {
		if row.version != firstMigrationVersion+i {
			return ledgerNotAPrefix, i
		}
	}
	for i, row := range recorded {
		if row.checksum != corpus[i].checksum {
			return ledgerFileChanged, i
		}
	}
	return ledgerUsable, 0
}

// refusedLedger renders one unusable verdict as the error an operator reads.
//
// Its last arm refuses rather than answering nil, ledgerUsable included: this renderer is reached
// only for a refusal, so being handed a verdict it cannot name means the caller has judged the
// ledger unusable and this function is about to let the migrations run anyway. That arm is
// reached directly by TestNoLedgerVerdictReachesTheRendererAndIsCarriedForwardSilently.
func refusedLedger(verdict ledgerVerdict, at int, recorded []appliedVersion,
	corpus []migration) error {
	switch verdict {
	case ledgerAheadOfBinary:
		return finished(schemaAhead(highestRecorded(recorded), highestEmbedded(corpus)))
	case ledgerNotAPrefix:
		return finished(fmt.Errorf("the ledger records version %d where an ascending run from %d "+
			"reaches %d, so it is a prefix of no corpus and applying the missing versions now would "+
			"apply them after a later one", recorded[at].version, firstMigrationVersion,
			firstMigrationVersion+at))
	case ledgerFileChanged:
		return finished(fmt.Errorf("migration %d (%s) was applied with checksum %s and its file now "+
			"hashes to %s, so the file has been edited since it was applied", corpus[at].version,
			corpus[at].file, recorded[at].checksum, corpus[at].checksum))
	}
	return finished(fmt.Errorf("the ledger was judged unusable for a reason this runner cannot "+
		"name (%s), so no migration was applied", verdict))
}

// highestRecorded is the version a database records, which ADR-5 defines as the maximum over the
// ledger -- the last row of an ascending read. Zero for a database that records none.
func highestRecorded(recorded []appliedVersion) int {
	if len(recorded) == 0 {
		return 0
	}
	return recorded[len(recorded)-1].version
}

// highestEmbedded is the highest version this binary carries, which is a different question from
// the one above and is why they are two functions rather than one over an interface.
func highestEmbedded(corpus []migration) int {
	if len(corpus) == 0 {
		return 0
	}
	return corpus[len(corpus)-1].version
}

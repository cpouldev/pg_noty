package schema

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// This file gets a database from whatever it records up to the highest version a corpus embeds:
// ascending, exactly once each, one transaction per migration, and refusing a database this binary
// does not know (criteria 11 to 16). runnerledger.go is the other half -- what the database records,
// and why a recorded ledger may not be carried forward.
//
// Two decisions the rest of the file rests on.
//
// The corpus is a parameter and is never reached for (ADR-10). That is what lets an induced-failure
// or partial-ledger case drive this runner rather than a double, without a fake migration being
// shipped to every customer database.
//
// searchPathStatement is the one place a configured name is interpolated into SQL on this whole
// path (D1), which is why the two .sql files and every statement the runner issues write
// unqualified names.

// txBeginner is all the runner needs of a connection. Both *pgxpool.Pool and *pgxpool.Conn satisfy
// it, so Step 13 may hand this runner the very connection it holds the session advisory lock on
// (ADR-4) without the runner having to know which it was given.
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// migrationRun is what one call to migrate did: the versions it applied, ascending, and the version
// the database records afterwards. applied is empty for a database that was already current, which
// is how criterion 15's caller sees that nothing was issued.
type migrationRun struct {
	applied  []int
	recorded int
}

// searchPathStatement is the migration path's only interpolation site (D1).
//
// SET LOCAL and not SET, and the difference is the whole design: the setting lasts exactly as long
// as the transaction that issued it, so migration 2 cannot inherit migration 1's routing and a
// pooled connection carries none of it back to the pool. Both halves are asserted as effects rather
// than as text, by the two-schema routing case and by
// TestTheSearchPathDoesNotOutliveTheTransactionThatSetIt.
//
// pg_catalog is named after the schema so a migration cannot be made to resolve a system name to
// something the service schema declares.
func searchPathStatement(schemaName string) (string, IdentifierFault) {
	name, fault := Quoted(schemaName)
	if fault != IdentifierOK {
		return "", fault
	}
	return "SET LOCAL search_path TO " + name + ", pg_catalog", IdentifierOK
}

// migrateEmbedded is migrate with the corpus this binary ships, and is the entry point Step 13's
// bootstrap composes (ADR-10's "defaulting to the embedded corpus").
//
// It is a separate declaration rather than a nil corpus falling back inside migrate, and the
// distinction is what ADR-10 is worth: migrate itself never reaches for the shipped corpus, so a
// synthetic one drives exactly the code a customer database gets, and a caller that meant to pass a
// corpus cannot silently be given this one instead. TestTheRunnerTakesItsCorpusAsAParameterRather
// ThanReachingForTheEmbeddedOne holds that division shut.
func migrateEmbedded(ctx context.Context, db txBeginner, schemaName string) (migrationRun, error) {
	corpus, err := embeddedCorpus()
	if err != nil {
		return migrationRun{}, finished(err)
	}
	return migrate(ctx, db, schemaName, corpus)
}

// migrate applies every migration in corpus the database does not already record, ascending.
//
// corpus is expected to be one corpusFrom produced: consecutive from the first version, with no
// duplicate. whyUnusableLedger's bounds argument rests on that, and corpusFrom is its only producer.
func migrate(ctx context.Context, db txBeginner, schemaName string,
	corpus []migration) (migrationRun, error) {
	setPath, fault := searchPathStatement(schemaName)
	if fault != IdentifierOK {
		return migrationRun{}, finished(fmt.Errorf("the configured schema %q %s", schemaName, fault))
	}

	recorded, err := readLedger(ctx, db, setPath)
	if err != nil {
		return migrationRun{}, err
	}
	if verdict, at := whyUnusableLedger(recorded, corpus); verdict != ledgerUsable {
		return migrationRun{}, refusedLedger(verdict, at, recorded, corpus)
	}

	// Safe because the verdict is usable: the ledger is then the prefix 1..len(recorded) of a
	// corpus that is itself 1..len(corpus), so the pending run is everything after it.
	run := migrationRun{recorded: highestRecorded(recorded)}
	for _, pending := range corpus[len(recorded):] {
		if err := applyMigration(ctx, db, setPath, pending); err != nil {
			return run, err
		}
		run.applied = append(run.applied, pending.version)
		run.recorded = pending.version
	}
	return run, nil
}

// applyMigration applies one migration and records it, in one transaction (D1). The search_path is
// the transaction's first statement, so every unqualified name in the migration text and in the
// ledger insert resolves in the configured schema; the insert is its last, so a migration that
// fails leaves behind no row claiming it succeeded. PostgreSQL rolls DDL back like any other
// statement, which is what makes the pair atomic rather than merely ordered.
func applyMigration(ctx context.Context, db txBeginner, setPath string, m migration) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return namedFailure(m, "open the transaction for", err)
	}
	// A Rollback after a successful Commit is a no-op, so no failure path below needs unwinding.
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, setPath); err != nil {
		return namedFailure(m, "route into its schema", err)
	}
	if err := m.apply(ctx, migrationExecOn(tx)); err != nil {
		return finished(err) // apply already names the version and the file
	}
	if _, err := tx.Exec(ctx, ledgerInsert, m.version, m.checksum); err != nil {
		return namedFailure(m, "record", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return namedFailure(m, "commit", err)
	}
	return nil
}

// namedFailure attributes one failure to the migration it happened in, because an operator told
// only which statement failed cannot tell which migration to fix (criterion 13).
func namedFailure(m migration, doing string, err error) error {
	return finished(fmt.Errorf("%s migration %d (%s): %w", doing, m.version, m.file, err))
}

// migrationExecOn is the runner's half of ADR-10's seam and its only simple-protocol call site
// (TestTheRunnerNamesTheSimpleProtocolWhereItSuppliesTheSeamAndNowhereElse).
//
// The mode is what lets one Exec carry a whole migration file, since pgx's default extended
// protocol accepts one statement per call. It is also the mode CVE-2026-41889 lived in, and the
// reason that class is inert here is measurable rather than argued: on pgx v5.10.0 the sanitizer
// the CVE was in is reached from execSimpleProtocol's `if len(arguments) > 0` branch alone, and
// migrationExec has no position at which an argument could be supplied. Selecting the mode is belt
// and braces, because that version also forces it whenever there are no arguments
// (TestTheDriverForcesTheSimpleProtocolWhenThereAreNoArguments) -- saying so explicitly is what
// keeps this path on it if that ever changes.
func migrationExecOn(tx pgx.Tx) migrationExec {
	return func(ctx context.Context, sql string) error {
		_, err := tx.Exec(ctx, sql, pgx.QueryExecModeSimpleProtocol)
		return err
	}
}

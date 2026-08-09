package schema

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cpouldev/pg_noty/internal/config"
)

// AppliedVersion reads the highest version recorded by the configured schema's ledger.
// The existing ledger reader owns the query, and an absent ledger is reported as ErrLedgerAbsent so
// that a caller which may not bootstrap can branch on it rather than on the message text.
func AppliedVersion(ctx context.Context, on txBeginner, cfg config.Config, opts Options) (int, error) {
	opts = opts.normalized()
	path, fault := searchPathStatement(cfg.Database.Schema)
	if fault != IdentifierOK {
		return 0, finished(fmt.Errorf("the configured schema %q %s", cfg.Database.Schema, fault))
	}
	recorded, err := readLedger(ctx, on, path)
	if err != nil {
		return 0, err
	}
	if recorded == nil {
		return 0, finished(ledgerAbsent(cfg.Database.Schema))
	}
	version := highestRecorded(recorded)
	opts.Logger.LogAttrs(ctx, slog.LevelDebug, "read applied schema version", slog.Int("version", version))
	return version, nil
}

// ExpectedVersion is the highest migration version this binary carries, which is what a database
// AppliedVersion agrees with is up to date. It is the same corpus Bootstrap migrates towards, read
// through the same reader, so the two answers cannot come from different inventories.
//
// It is exported because a caller that may not bootstrap still has to be able to tell "current"
// from "behind": AppliedVersion alone answers only whether the ledger exists.
func ExpectedVersion() (int, error) {
	corpus, err := embeddedCorpus()
	if err != nil {
		return 0, finished(err)
	}
	return highestEmbedded(corpus), nil
}

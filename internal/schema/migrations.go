package schema

import (
	"cmp"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

const (
	// migrationDir is the directory the corpus is embedded from, and the prefix every read joins.
	migrationDir = "migrations"
	// migrationSuffix is the only extension a migration file may carry; versionOf records why an
	// entry without it is refused rather than skipped.
	migrationSuffix = ".sql"
	// firstMigrationVersion is where the sequence starts. It is named because two refusals read it
	// -- a version below it, and a run that does not begin there -- and a bare 1 in each would not
	// say they are the same rule.
	firstMigrationVersion = 1
)

// The four reasons a corpus is refused, one answer per reason rather than one "invalid corpus" for
// all of them: an operator told only that the corpus is invalid cannot tell which file to fix.
//
// errCorpusVersionRange is not one of the three malformations the step enumerates. It closes the
// class those three are reproductions of, so a version that parses but cannot be a version -- 0, or
// a negative -- is refused as what it is rather than reported as the gap it also creates.
var (
	errCorpusFilename     = errors.New("migration filename declares no version")
	errCorpusVersionRange = errors.New("migration version is below the first version")
	errCorpusDuplicate    = errors.New("two migration files declare one version")
	errCorpusGap          = errors.New("migration versions are not consecutive")
)

// migration is one forward-only migration: the version its *filename* declares, the exact bytes
// embedded for it, and the sha256 over those bytes.
//
// The version comes from the filename so the order a corpus applies in is auditable without reading
// any SQL. The checksum exists for one purpose -- detecting a migration file edited after it was
// already applied, which Step 10's runner reports by comparing it against the ledger row naming the
// same version -- and Flyway's schema-history table is the prior art for carrying it there. It is
// not a hash of convenience: because both files carry unqualified names and no placeholder (D1),
// the bytes it covers are exactly the bytes that were executed.
type migration struct {
	version  int
	file     string
	sql      string
	checksum string
}

// embeddedCorpus is the corpus this binary ships. Step 10's runner takes []migration as a parameter
// and defaults to this (ADR-10), so an induced-failure or partial-ledger test can drive the real
// runner with a synthetic corpus without shipping a fake migration.
func embeddedCorpus() ([]migration, error) {
	return corpusFrom(embeddedMigrations, migrationDir)
}

// corpusFrom reads every migration in one directory of one filesystem, orders it ascending by
// filename-derived version, and refuses the four malformations above.
//
// The order of the checks is the order of the refusals, deliberately: a filename declaring no
// version is reported as that rather than as the gap it also creates, and two files declaring one
// version are reported as a duplicate rather than as the gap a duplicate always creates.
// TestTheDocumentedRefusalPrecedenceIsWhatSelects gives each pairing a corpus breaking both of its
// rules at once, without which both orders answer identically. A refused corpus returns none of
// itself, so no caller can read a partially validated one.
func corpusFrom(files fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(files, dir)
	if err != nil {
		return nil, fmt.Errorf("read the migration directory %s: %w", dir, err)
	}

	corpus := make([]migration, 0, len(entries))
	for _, entry := range entries {
		read, err := readMigration(files, dir, entry.Name())
		if err != nil {
			return nil, err
		}
		corpus = append(corpus, read)
	}
	slices.SortFunc(corpus, func(a, b migration) int { return cmp.Compare(a.version, b.version) })

	if err := duplicateVersionIn(corpus); err != nil {
		return nil, err
	}
	if err := versionGapIn(corpus); err != nil {
		return nil, err
	}
	return corpus, nil
}

// readMigration reads one entry of a corpus directory.
func readMigration(files fs.FS, dir, name string) (migration, error) {
	version, err := versionOf(name)
	if err != nil {
		return migration{}, err
	}

	data, err := fs.ReadFile(files, path.Join(dir, name))
	if err != nil {
		return migration{}, fmt.Errorf("read migration %s: %w", name, err)
	}
	sum := sha256.Sum256(data)
	return migration{
		version:  version,
		file:     name,
		sql:      string(data),
		checksum: hex.EncodeToString(sum[:]),
	}, nil
}

// versionOf is the version a migration filename declares, from a name shaped
// <version>_<description>.sql. Anything else is refused, including a directory and a stray note
// beside the migrations, because the alternative is skipping it.
func versionOf(name string) (int, error) {
	described, isSQL := strings.CutSuffix(name, migrationSuffix)
	numbered, _, hasDescription := strings.Cut(described, "_")
	if !isSQL || !hasDescription || numbered == "" {
		return 0, fmt.Errorf("%w: %q is not <version>_<description>%s",
			errCorpusFilename, name, migrationSuffix)
	}

	version, err := strconv.Atoi(numbered)
	if err != nil {
		return 0, fmt.Errorf("%w: %q begins %q, which is no number",
			errCorpusFilename, name, numbered)
	}
	if version < firstMigrationVersion {
		return 0, fmt.Errorf("%w: %q declares version %d, and the sequence starts at %d",
			errCorpusVersionRange, name, version, firstMigrationVersion)
	}
	return version, nil
}

// duplicateVersionIn refuses two files declaring one version, on a corpus already sorted. Two rows
// for one version in the ledger is the observation ADR-5's primary key exists to make impossible,
// and a corpus that could produce them is refused before any of it is applied.
func duplicateVersionIn(corpus []migration) error {
	for i := 1; i < len(corpus); i++ {
		if corpus[i].version == corpus[i-1].version {
			return fmt.Errorf("%w: %q and %q both declare version %d",
				errCorpusDuplicate, corpus[i-1].file, corpus[i].file, corpus[i].version)
		}
	}
	return nil
}

// versionGapIn refuses a corpus whose versions are not the consecutive run starting at the first
// version, on a corpus already sorted. A gap makes "the ledger accounts for every version 1..N"
// unanswerable, and criterion 16 compares the recorded set against that range rather than its
// maximum precisely so a gap cannot pass.
func versionGapIn(corpus []migration) error {
	for i, found := range corpus {
		if want := firstMigrationVersion + i; found.version != want {
			return fmt.Errorf("%w: %q declares version %d where the ascending run reaches %d",
				errCorpusGap, found.file, found.version, want)
		}
	}
	return nil
}

// migrationExec executes one migration's text against an open transaction. The signature is the
// invariant rather than a convention: it has no `args ...any`, so there is no position at which a
// caller could supply a bind parameter (ADR-10, M13).
type migrationExec func(ctx context.Context, sql string) error

// apply executes m's text through exec, and does nothing else -- the ledger row and the
// transaction are Step 10's runner's.
//
// This is the call site the no-arguments invariant is about, so the reason lives here. A migration
// file holds many statements and pgx's default extended protocol accepts one per call, so the
// closure the runner supplies must select pgx.QueryExecModeSimpleProtocol. That mode is where
// CVE-2026-41889 lived, fixed in pgx v5.9.2: its sanitizer mishandled a $1-shaped token inside a
// dollar-quoted literal and substituted caller-supplied parameter data into it. The precondition is
// a bind parameter, and this signature has none to give -- the mode travels inside the closure,
// where pgx strips it before any argument is bound, so selecting simple protocol costs nothing
// here. Weakening this means adding `args ...any`, at which point the precondition is back on the
// path and this reasoning has to be redone rather than inherited.
func (m migration) apply(ctx context.Context, exec migrationExec) error {
	if err := exec(ctx, m.sql); err != nil {
		return fmt.Errorf("migration %d (%s): %w", m.version, m.file, err)
	}
	return nil
}

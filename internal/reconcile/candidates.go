package reconcile

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Candidate is one registry/catalog object pair handed to DetermineOwnership by the CLI.
type Candidate struct {
	Registry RegistryRow
	Catalog  CatalogObject
	identity string
}

// OwnedCandidates composes the complete registry reader and catalog's existing object readers in
// one read-only transaction. Configured listeners supply the catalog-only direction.
func OwnedCandidates(ctx context.Context, on *pgxpool.Pool, cfg config.Config, opts Options) ([]Candidate, error) {
	if on == nil {
		return nil, fmt.Errorf("candidate read has no pool")
	}
	tx, err := on.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin candidate read: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	readings, err := readRegistries(ctx, tx, cfg.Database.Schema)
	if err != nil {
		return nil, err
	}
	catalog := NewCatalog(tx)
	var candidates []Candidate
	for _, reading := range readings {
		candidates, err = appendRecordedCandidates(ctx, catalog, reading, candidates)
		if err != nil {
			return nil, err
		}
	}
	for _, listener := range cfg.Listeners {
		candidates, err = appendConfiguredCandidates(ctx, catalog, cfg, listener, candidates)
		if err != nil {
			return nil, err
		}
	}
	optionsLogger(opts).LogAttrs(
		ctx, slog.LevelDebug, "read ownership candidates",
		slog.Int("count", len(candidates)),
	)
	return deduplicateCandidates(candidates), nil
}

// appendPair reads one trigger and, when it is there, the function it points at. The two passes
// below were near-identical copies until one of them passed the trigger's own oid to ReadFunction,
// which matches pg_proc.oid, and destroy refused every healthy install.
//
// absentRecordsTheRow is the one behaviour they do not share: the recorded pass keeps a bare
// registry candidate for a trigger the catalog no longer holds, which is itself a disagreement.
func appendPair(
	ctx context.Context, catalog Catalog, row RegistryRow, targetOID uint32,
	triggerName, functionName string, absentRecordsTheRow bool, into []Candidate,
) ([]Candidate, error) {
	trigger, found, err := catalog.ReadTrigger(ctx, targetOID, triggerName)
	if err != nil {
		return nil, err
	}
	into = append(into, candidateForTrigger(row, triggerName, trigger, found))
	if !found {
		if absentRecordsTheRow {
			into = append(into, Candidate{Registry: row})
		}
		return into, nil
	}
	function, functionFound, err := catalog.ReadFunction(ctx, trigger.FunctionOID)
	if err != nil {
		return nil, err
	}
	return append(into, candidateForFunction(row, functionName, function, functionFound)), nil
}

func appendRecordedCandidates(
	ctx context.Context,
	catalog Catalog,
	reading registryReading,
	into []Candidate,
) ([]Candidate, error) {
	for _, trigger := range reading.Triggers {
		row := registryOwnershipRow(
			recordedPair{
				Pair: Pair{
					Listener: reading.Listener.Name, Operation: trigger.Operation,
				}, TriggerPresent: true,
			},
		)
		var err error
		if into, err = appendPair(
			ctx, catalog, row, reading.Listener.TargetOID,
			trigger.TriggerName, trigger.FunctionName, true, into,
		); err != nil {
			return nil, err
		}
	}
	return into, nil
}

func appendConfiguredCandidates(
	ctx context.Context,
	catalog Catalog,
	cfg config.Config,
	listener config.Listener,
	into []Candidate,
) ([]Candidate, error) {
	target, found, err := catalog.ResolveTarget(ctx, listener.Trigger.Table)
	if err != nil || !found {
		return into, err
	}
	compiled, err := compileListener(cfg.Instance, cfg.Database.Schema, listener, target)
	if err != nil {
		return nil, err
	}
	for _, set := range compiled.Sets {
		row := registryOwnershipRow(recordedPair{Pair: Pair{Listener: listener.Name, Operation: set.Operation}})
		if into, err = appendPair(
			ctx, catalog, row, target.OID,
			set.TriggerName, set.FunctionName, false, into,
		); err != nil {
			return nil, err
		}
	}
	return into, nil
}

func candidateForTrigger(row RegistryRow, name string, reading TriggerReading, found bool) Candidate {
	if !found {
		return Candidate{Registry: row, identity: "trigger:" + name}
	}
	return Candidate{
		Registry: row, Catalog: catalogTriggerObject(reading, "trigger "+name), identity: "trigger:" + name,
	}
}

func candidateForFunction(row RegistryRow, name string, reading FunctionReading, found bool) Candidate {
	if !found {
		return Candidate{Registry: row, identity: "function:" + name}
	}
	return Candidate{
		Registry: row, Catalog: catalogFunctionObject(reading, "function "+name), identity: "function:" + name,
	}
}

func deduplicateCandidates(candidates []Candidate) []Candidate {
	seen := make(map[string]bool, len(candidates))
	unique := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		key := candidate.identity + ":" + candidate.Registry.Listener + ":" + candidate.Registry.Operation
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, candidate)
	}
	return unique
}

package reconcile

import (
	"context"
	"fmt"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
)

type targetOutcome string

const (
	targetUnresolved          targetOutcome = ""
	targetNew                 targetOutcome = "new"
	targetHealthy             targetOutcome = "healthy"
	targetRenamed             targetOutcome = "renamed"
	targetDropped             targetOutcome = "dropped"
	targetDroppedAndRecreated targetOutcome = "dropped-and-recreated"
)

var targetOutcomes = [...]targetOutcome{
	targetNew, targetHealthy, targetRenamed, targetDropped, targetDroppedAndRecreated,
}

type targetState struct {
	recorded, oid, named, sameName bool
}

type targetResolution struct {
	Outcome targetOutcome
	Target  TargetReading
	Refusal *Refusal
}

// resolveListenerTarget reads both identity signals through Catalog. TableExists uses this same
// resolution, keeping one shared meaning of a target rather than a reconstructed one.
func resolveListenerTarget(
	ctx context.Context,
	catalog Catalog,
	listener config.Listener,
	recorded *registryListener,
) (targetResolution, error) {
	name := listener.Trigger.Table
	if recorded == nil {
		reading, found, err := catalog.ResolveTarget(ctx, name)
		return resolvedNewTarget(listener, name, reading, found, err)
	}
	byOID, oidFound, err := catalog.ResolveTargetOID(ctx, recorded.TargetOID)
	if err != nil {
		return targetResolution{}, err
	}
	// Every signal below is read from the recorded target, so a configured table pointing somewhere
	// else would never be consulted. The test is identity, not text: the configured name is written
	// as the author typed it and the recorded one is stored qualified, so comparing the two strings
	// answers a spelling question. It is also not enough that the OIDs differ -- a target dropped and
	// recreated keeps its name and gains a new OID, and that case is adopted rather than refused.
	//
	// A move is the case where both tables exist and they are not the same table.
	byName, nameFound, err := catalog.ResolveTarget(ctx, recorded.TargetTable)
	if err != nil {
		return targetResolution{}, err
	}
	// Only when the two spellings differ is a third lookup worth its round trip: equal names cannot
	// be a move, and the recorded-name lookup above has already answered for them.
	byConfigured, configuredFound := byName, nameFound
	if name != recorded.TargetTable {
		if byConfigured, configuredFound, err = catalog.ResolveTarget(ctx, name); err != nil {
			return targetResolution{}, err
		}
	}
	if oidFound && configuredFound && byConfigured.OID != recorded.TargetOID {
		refusal := retargetedListenerRefusal(listener.Name, recorded.TargetTable, name)
		return targetResolution{Outcome: targetDropped, Refusal: &refusal}, nil
	}
	sameName, err := targetHasRecordedName(byOID, oidFound, recorded.TargetTable)
	if err != nil {
		return targetResolution{}, err
	}
	state := targetState{recorded: true, oid: oidFound, named: nameFound, sameName: sameName}
	return resolvedRecordedTarget(listener, recorded.TargetTable, byOID, byName, classifyTarget(state)), nil
}

func resolvedNewTarget(
	listener config.Listener,
	name string,
	reading TargetReading,
	found bool,
	err error,
) (targetResolution, error) {
	if err != nil {
		return targetResolution{}, err
	}
	outcome := classifyTarget(targetState{named: found})
	if outcome == targetDropped {
		return droppedResolution(listener.Name, name), nil
	}
	return targetResolution{Outcome: outcome, Target: reading}, nil
}

func resolvedRecordedTarget(
	listener config.Listener,
	name string,
	byOID, byName TargetReading,
	outcome targetOutcome,
) targetResolution {
	if outcome == targetDropped {
		return droppedResolution(listener.Name, name)
	}
	if outcome == targetDroppedAndRecreated {
		return targetResolution{Outcome: outcome, Target: byName}
	}
	return targetResolution{Outcome: outcome, Target: byOID}
}

func droppedResolution(listener, table string) targetResolution {
	refusal := droppedTargetRefusal(listener, table)
	return targetResolution{Outcome: targetDropped, Refusal: &refusal}
}

func targetHasRecordedName(reading TargetReading, found bool, recorded string) (bool, error) {
	if !found {
		return false, nil
	}
	name, fault := schema.Qualified(reading.Schema, reading.Table)
	if fault != schema.IdentifierOK {
		return false, fmt.Errorf("catalog target %q.%q is unusable: %s", reading.Schema, reading.Table, fault)
	}
	return name == recorded, nil
}

func classifyTarget(state targetState) targetOutcome {
	if !state.recorded && state.named {
		return targetNew
	}
	if state.oid && state.sameName {
		return targetHealthy
	}
	if state.oid {
		return targetRenamed
	}
	if state.named {
		return targetDroppedAndRecreated
	}
	return targetDropped
}

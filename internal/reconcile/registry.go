package reconcile

import (
	"context"
	"fmt"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/jackc/pgx/v5"
)

type registryListener struct {
	Name        string
	Spec        []byte
	SpecHash    string
	TargetTable string
	TargetOID   uint32
	Enabled     bool
	AppliedAt   time.Time
}

type registryTrigger struct {
	Operation    string
	TriggerName  string
	FunctionName string
	DDLHash      string
}

type registryReading struct {
	Listener registryListener
	Triggers []registryTrigger
}

type registryReader interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// writeRegistry participates in the caller-owned apply transaction. applied_at comes from the
// database clock, so separate reconcilers never infer server provenance from their local clocks.
func writeRegistry(
	ctx context.Context,
	tx pgx.Tx,
	serviceSchema string,
	listener registryListener,
	triggers []registryTrigger,
) (registryListener, error) {
	listenerTable, err := qualifiedRegistryTable(serviceSchema, schema.TableListeners)
	if err != nil {
		return registryListener{}, err
	}
	triggerRows, err := qualifiedRegistryTable(serviceSchema, schema.TableListenerTriggers)
	if err != nil {
		return registryListener{}, err
	}
	target, err := canonicalTarget(listener.TargetTable)
	if err != nil {
		return registryListener{}, err
	}
	result, err := tx.Exec(
		ctx,
		"INSERT INTO "+listenerTable+" AS stored (name, spec, spec_hash, target_table, target_oid, enabled, applied_at) VALUES ($1, $2, $3, $4, $5, $6, now()) ON CONFLICT (name) DO UPDATE SET spec = EXCLUDED.spec, spec_hash = EXCLUDED.spec_hash, target_table = EXCLUDED.target_table, target_oid = EXCLUDED.target_oid, enabled = EXCLUDED.enabled, applied_at = now() WHERE (stored.spec, stored.spec_hash, stored.target_table, stored.target_oid, stored.enabled) IS DISTINCT FROM (EXCLUDED.spec, EXCLUDED.spec_hash, EXCLUDED.target_table, EXCLUDED.target_oid, EXCLUDED.enabled)",
		listener.Name,
		string(listener.Spec),
		listener.SpecHash,
		target,
		listener.TargetOID,
		listener.Enabled,
	)
	if err != nil {
		return registryListener{}, err
	}
	listenerChanged := result.RowsAffected() != 0
	changed := listenerChanged
	operations := make([]string, 0, len(triggers))
	for _, trigger := range triggers {
		operations = append(operations, trigger.Operation)
		result, err := tx.Exec(
			ctx,
			"INSERT INTO "+triggerRows+" AS stored (listener, operation, trigger_name, function_name, ddl_hash) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (listener, operation) DO UPDATE SET trigger_name = EXCLUDED.trigger_name, function_name = EXCLUDED.function_name, ddl_hash = EXCLUDED.ddl_hash WHERE (stored.trigger_name, stored.function_name, stored.ddl_hash) IS DISTINCT FROM (EXCLUDED.trigger_name, EXCLUDED.function_name, EXCLUDED.ddl_hash)",
			listener.Name,
			trigger.Operation,
			trigger.TriggerName,
			trigger.FunctionName,
			trigger.DDLHash,
		)
		if err != nil {
			return registryListener{}, err
		}
		changed = changed || result.RowsAffected() != 0
	}
	result, err = tx.Exec(
		ctx,
		"DELETE FROM "+triggerRows+" WHERE listener = $1 AND operation <> ALL($2::text[])",
		listener.Name,
		operations,
	)
	if err != nil {
		return registryListener{}, err
	}
	changed = changed || result.RowsAffected() != 0
	if changed && !listenerChanged {
		if _, err := tx.Exec(
			ctx,
			"UPDATE "+listenerTable+" SET applied_at = now() WHERE name = $1",
			listener.Name,
		); err != nil {
			return registryListener{}, err
		}
	}
	reading, err := readRegistry(ctx, tx, serviceSchema, listener.Name)
	if err != nil {
		return registryListener{}, err
	}
	return reading.Listener, nil
}

// readRegistry returns the complete persistent representation for one listener.
func readRegistry(ctx context.Context, pool registryReader, serviceSchema, name string) (registryReading, error) {
	listenerTable, err := qualifiedRegistryTable(serviceSchema, schema.TableListeners)
	if err != nil {
		return registryReading{}, err
	}
	triggerRows, err := qualifiedRegistryTable(serviceSchema, schema.TableListenerTriggers)
	if err != nil {
		return registryReading{}, err
	}
	var reading registryReading
	if err := pool.QueryRow(
		ctx,
		"SELECT name, spec, spec_hash, target_table, target_oid, enabled, applied_at FROM "+listenerTable+" WHERE name = $1",
		name,
	).Scan(
		&reading.Listener.Name,
		&reading.Listener.Spec,
		&reading.Listener.SpecHash,
		&reading.Listener.TargetTable,
		&reading.Listener.TargetOID,
		&reading.Listener.Enabled,
		&reading.Listener.AppliedAt,
	); err != nil {
		return registryReading{}, err
	}
	if _, _, ok := source.SplitQualified(reading.Listener.TargetTable); !ok {
		return registryReading{}, fmt.Errorf("registry listener %q has an invalid stored target table", name)
	}
	rows, err := pool.Query(
		ctx,
		"SELECT operation, trigger_name, function_name, ddl_hash FROM "+triggerRows+" WHERE listener = $1 ORDER BY operation",
		name,
	)
	if err != nil {
		return registryReading{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var trigger registryTrigger
		if err := rows.Scan(
			&trigger.Operation,
			&trigger.TriggerName,
			&trigger.FunctionName,
			&trigger.DDLHash,
		); err != nil {
			return registryReading{}, err
		}
		reading.Triggers = append(reading.Triggers, trigger)
	}
	if err := rows.Err(); err != nil {
		return registryReading{}, err
	}
	return reading, nil
}

// readRegistries returns every stored record and its operations. It is the only complete registry
// read: planning must see records absent from configuration so their objects cannot be left behind.
func readRegistries(ctx context.Context, pool registryReader, serviceSchema string) ([]registryReading, error) {
	listenerTable, err := qualifiedRegistryTable(serviceSchema, schema.TableListeners)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, "SELECT name FROM "+listenerTable+" ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	readings := make([]registryReading, 0, len(names))
	for _, name := range names {
		reading, err := readRegistry(ctx, pool, serviceSchema, name)
		if err != nil {
			return nil, err
		}
		readings = append(readings, reading)
	}
	return readings, nil
}

func registryByName(readings []registryReading) map[string]registryReading {
	byName := make(map[string]registryReading, len(readings))
	for _, reading := range readings {
		byName[reading.Listener.Name] = reading
	}
	return byName
}

// deleteRegistry removes one stored record; the database cascades its operation rows.
func deleteRegistry(ctx context.Context, tx pgx.Tx, serviceSchema, name string) error {
	table, err := qualifiedRegistryTable(serviceSchema, schema.TableListeners)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "DELETE FROM "+table+" WHERE name = $1", name)
	return err
}

func qualifiedRegistryTable(serviceSchema, table string) (string, error) {
	qualified, fault := schema.Qualified(serviceSchema, table)
	if fault != schema.IdentifierOK {
		return "", fmt.Errorf("registry table name is unusable: %s", fault)
	}
	return qualified, nil
}

func canonicalTarget(stored string) (string, error) {
	targetSchema, targetTable, ok := source.SplitQualified(stored)
	if !ok {
		return "", fmt.Errorf("registry target table has an invalid qualified spelling")
	}
	target, fault := schema.Qualified(targetSchema, targetTable)
	if fault != schema.IdentifierOK {
		return "", fmt.Errorf("registry target name is unusable: %s", fault)
	}
	return target, nil
}

package reconcile

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Catalog is the sole read-only system-catalog authority; catalogqueries.go is its query half.
type Catalog struct {
	on      catalogReader
	session catalogSession
}
type catalogReader interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type catalogSession interface {
	Begin(context.Context) (pgx.Tx, error)
}

const catalogSearchPathPin = "SET LOCAL search_path = ''"

func NewCatalog(on pgx.Tx) Catalog { return Catalog{on: on} }

func NewConnectionCatalog(on *pgxpool.Conn) Catalog { return Catalog{on: on, session: on} }

type TargetReading struct {
	OID               uint32
	Schema            string
	Table             string
	Columns           []string
	PrimaryKeyColumns []string
}
type TriggerReading struct {
	OID, FunctionOID uint32
	Marker           *string
	Definition       string
}
type FunctionReading struct {
	OID        uint32
	Marker     *string
	Definition string
}
type PairReading struct {
	Trigger  TriggerReading
	Function FunctionReading
}

type LockReading struct {
	Mode    string
	Granted bool
}

// ResolveTarget resolves configuredName, then reads its catalog spelling and columns back from that OID; target resolution and TableExists share it.
func (catalog Catalog) ResolveTarget(ctx context.Context, configuredName string) (TargetReading, bool, error) {
	return catalog.resolveTarget(ctx, nil, configuredName)
}

// ResolveTargetOID resolves a recorded relation OID and returns the catalog's current spelling.
func (catalog Catalog) ResolveTargetOID(ctx context.Context, oid uint32) (TargetReading, bool, error) {
	return catalog.resolveTarget(ctx, oid, nil)
}

func (catalog Catalog) resolveTarget(ctx context.Context, oid any, configuredName any) (TargetReading, bool, error) {
	var target TargetReading
	err := catalog.pinSearchPath(ctx, func(on catalogReader) error {
		return on.QueryRow(ctx, resolveTargetQuery, oid, configuredName).Scan(
			&target.OID, &target.Schema, &target.Table, &target.Columns, &target.PrimaryKeyColumns,
		)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return TargetReading{}, false, nil
	}
	if err != nil {
		return TargetReading{}, false, fmt.Errorf("resolve catalog target: %w", err)
	}
	return target, true, nil
}

func (catalog Catalog) ReadTrigger(ctx context.Context, targetOID uint32, name string) (TriggerReading, bool, error) {
	var reading TriggerReading
	err := catalog.pinSearchPath(ctx, func(on catalogReader) error {
		return on.QueryRow(ctx, readTriggerQuery, targetOID, name).Scan(&reading.OID, &reading.FunctionOID, &reading.Marker, &reading.Definition)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return TriggerReading{}, false, nil
	} else if err != nil {
		return TriggerReading{}, false, fmt.Errorf("read trigger: %w", err)
	}
	return reading, true, nil
}

func (catalog Catalog) ReadFunction(ctx context.Context, oid uint32) (FunctionReading, bool, error) {
	var reading FunctionReading
	err := catalog.pinSearchPath(ctx, func(on catalogReader) error {
		return on.QueryRow(ctx, readFunctionQuery, oid).Scan(&reading.OID, &reading.Marker, &reading.Definition)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return FunctionReading{}, false, nil
	} else if err != nil {
		return FunctionReading{}, false, fmt.Errorf("read function: %w", err)
	}
	return reading, true, nil
}

func (catalog Catalog) ReadPair(ctx context.Context, targetOID uint32, triggerName string) (PairReading, bool, error) {
	trigger, found, err := catalog.ReadTrigger(ctx, targetOID, triggerName)
	if err != nil || !found {
		return PairReading{}, false, err
	}
	function, found, err := catalog.ReadFunction(ctx, trigger.FunctionOID)
	if err != nil || !found {
		return PairReading{}, false, err
	}
	return PairReading{Trigger: trigger, Function: function}, true, nil
}

func (catalog Catalog) ReadLocks(ctx context.Context, relationOID uint32) ([]LockReading, error) {
	var locks []LockReading
	err := catalog.pinSearchPath(ctx, func(on catalogReader) error {
		rows, err := on.Query(ctx, readLocksQuery, relationOID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var lock LockReading
			if err := rows.Scan(&lock.Mode, &lock.Granted); err != nil {
				return err
			}
			locks = append(locks, lock)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("read relation locks: %w", err)
	}
	return locks, nil
}

func (catalog Catalog) SchemaExists(ctx context.Context, schema string) (bool, error) {
	var exists bool
	err := catalog.pinSearchPath(ctx, func(on catalogReader) error {
		return on.QueryRow(ctx, schemaExistsQuery, schema).Scan(&exists)
	})
	if err != nil {
		return false, fmt.Errorf("read catalog schema: %w", err)
	}
	return exists, nil
}

func (catalog Catalog) AdvisoryLockHolder(ctx context.Context, key int64) (string, bool, error) {
	high, low := catalogAdvisoryKeyParts(key)
	return catalog.readBackend(ctx, "read advisory lock holder", advisoryLockHolderQuery, high, low)
}

func (catalog Catalog) BlockingBackend(ctx context.Context, backendID int32) (string, bool, error) {
	return catalog.readBackend(ctx, "read blocking backend", blockingBackendQuery, backendID)
}

func (catalog Catalog) readBackend(ctx context.Context, operation, query string, arguments ...any) (string, bool, error) {
	var backend string
	err := catalog.pinSearchPath(ctx, func(on catalogReader) error {
		return on.QueryRow(ctx, query, arguments...).Scan(&backend)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("%s: %w", operation, err)
	}
	return backend, true, nil
}

func catalogAdvisoryKeyParts(key int64) (uint32, uint32) {
	unsigned := uint64(key)
	return uint32(unsigned >> 32), uint32(unsigned)
}

func (catalog Catalog) pinSearchPath(ctx context.Context, observe func(catalogReader) error) error {
	on := catalog.on
	if catalog.session != nil {
		tx, err := catalog.session.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin catalog read: %w", err)
		}
		defer tx.Rollback(context.Background())
		on = tx
	}
	if _, err := on.Exec(ctx, catalogSearchPathPin); err != nil {
		return fmt.Errorf("pin catalog search_path: %w", err)
	}
	return observe(on)
}

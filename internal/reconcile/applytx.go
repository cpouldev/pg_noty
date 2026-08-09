package reconcile

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This is the recorded twin of internal/schema/ddl.go's boundedTx and lockTimeoutStatement.
// Its property differs: one transaction is held for the whole reconciliation run, not per call,
// so a partial apply is unrepresentable. A pooled connection outlives each statement it ran, so a
// session-level bound would silently govern the next caller's unrelated work.
const applyLockTimeout = 3 * time.Second

type applyBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// applyObject is the already-compiled create-side inventory for one target operation.
type applyObject struct {
	Target                                   TargetReading
	ServiceSchema, TriggerName, FunctionName string
	Drop                                     bool
	Creates                                  []string
}

type applyTx struct {
	tx      pgx.Tx
	monitor *pgxpool.Pool
	record  func(string)
	changes int
}

// applyInTransaction opens the one apply transaction with its two mandatory local settings first.
func applyInTransaction(ctx context.Context, on applyBeginner, monitor *pgxpool.Pool, record func(string), work func(*applyTx) error) (int, error) {
	return applyInTransactionWithTimeout(ctx, on, monitor, applyLockTimeout, record, work)
}

func applyInTransactionWithTimeout(ctx context.Context, on applyBeginner, monitor *pgxpool.Pool, bound time.Duration, record func(string), work func(*applyTx) error) (int, error) {
	tx, err := on.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin apply transaction: %w", err)
	}
	defer tx.Rollback(context.Background())
	run := &applyTx{tx: tx, monitor: monitor, record: record}
	if err = run.local(ctx, catalogSearchPathPin); err == nil {
		err = run.local(ctx, applyLockTimeoutStatement(bound))
	}
	if err == nil {
		err = work(run)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		return run.changes, err
	}
	return run.changes, nil
}

func (run *applyTx) local(ctx context.Context, statement string) error {
	if run.record != nil {
		run.record(statement)
	}
	_, err := run.tx.Exec(ctx, statement)
	return err
}

// apply groups catalog targets before execution: a reversed configuration cannot change lock order.
func (run *applyTx) apply(ctx context.Context, objects []applyObject) error {
	groups, err := groupedApplyObjects(objects)
	if err != nil {
		return err
	}
	for _, group := range groups {
		if err = run.applyGroup(ctx, group); err != nil {
			return err
		}
	}
	return nil
}

type applyGroup struct {
	name    string
	target  TargetReading
	objects []applyObject
}

func (run *applyTx) applyGroup(ctx context.Context, group applyGroup) error {
	if err := run.applyDrops(ctx, group); err != nil {
		return err
	}
	return run.applyCreates(ctx, group)
}

func (run *applyTx) applyDrops(ctx context.Context, group applyGroup) error {
	for _, object := range group.objects {
		if object.Drop {
			if err := run.drop(ctx, object); err != nil {
				return err
			}
		}
	}
	return nil
}

func (run *applyTx) applyCreates(ctx context.Context, group applyGroup) error {
	for _, object := range group.objects {
		for _, statement := range object.Creates {
			if err := run.change(ctx, group.target, statement); err != nil {
				return err
			}
		}
	}
	return nil
}

func groupedApplyObjects(objects []applyObject) ([]applyGroup, error) {
	byTarget := map[string]*applyGroup{}
	for _, object := range objects {
		name, err := ddlQualified("target table", object.Target.Schema, object.Target.Table)
		if err != nil {
			return nil, err
		}
		group := byTarget[name]
		if group == nil {
			group = &applyGroup{name: name, target: object.Target}
			byTarget[name] = group
		}
		group.objects = append(group.objects, object)
	}
	groups := make([]applyGroup, 0, len(byTarget))
	for _, group := range byTarget {
		groups = append(groups, *group)
	}
	slices.SortFunc(groups, func(left, right applyGroup) int { return strings.Compare(left.name, right.name) })
	return groups, nil
}

func (run *applyTx) drop(ctx context.Context, object applyObject) error {
	trigger, err := dropTriggerText(object.Target, object.TriggerName)
	if err != nil {
		return err
	}
	if err = run.change(ctx, object.Target, trigger); err != nil {
		return err
	}
	function, err := dropFunctionText(object.ServiceSchema, object.FunctionName)
	if err != nil {
		return err
	}
	return run.change(ctx, object.Target, function)
}

func (run *applyTx) change(ctx context.Context, target TargetReading, statement string) error {
	if run.record != nil {
		run.record(statement)
	}
	watch := startBlockerWatch(ctx, run.monitor, int32(run.tx.Conn().PgConn().PID()))
	_, err := run.tx.Exec(ctx, statement)
	observed := watch.stop()
	if err == nil {
		run.changes++
		return nil
	}
	if lockTimedOut(err) {
		table, tableErr := ddlQualified("target table", target.Schema, target.Table)
		if tableErr != nil {
			return tableErr
		}
		return errors.New(tableLockTimeoutRefusal(table, observed.description()).Message())
	}
	return err
}

func applyLockTimeoutStatement(bound time.Duration) string {
	if bound <= 0 {
		bound = applyLockTimeout
	}
	milliseconds := bound.Milliseconds()
	if milliseconds < 1 {
		milliseconds = 1
	}
	return "SET LOCAL lock_timeout = " + strconv.FormatInt(milliseconds, 10)
}

func lockTimedOut(err error) bool {
	var server *pgconn.PgError
	return errors.As(err, &server) && server.Code == "55P03"
}

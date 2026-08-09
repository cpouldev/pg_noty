package source

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func (s *TriggerSource) RetryBatch(ctx context.Context, selector QueueSelector, limit int) (int, error) {
	if err := s.ensureUsable(); err != nil {
		return 0, err
	}
	if limit <= 0 {
		return 0, fmt.Errorf("retry batch limit must be positive")
	}
	where, args, err := selectorPredicate(selector, 2, true)
	if err != nil {
		return 0, err
	}
	queue, err := qualifiedServiceTable(s.cfg.Database.Schema, schema.TableEventQueue)
	if err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin retry batch: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	// alreadyRetried excludes the rows this statement would leave unchanged. Without it `--status
	// pending` is a fixed point -- the predicate selects on the status the assignment sets -- so a
	// caller draining until zero never terminates.
	alreadyRetried := ` AND NOT (status=` + quoteLiteral("pending") +
		` AND attempts=0 AND dead_reason IS NULL AND next_attempt_at <= now())`
	query := `UPDATE ` + queue + ` q SET status=` + quoteLiteral("pending") + `, attempts=0, next_attempt_at=now(), dead_reason=NULL
WHERE q.event_id IN (SELECT event_id FROM ` + queue + ` WHERE` + where + alreadyRetried + ` ORDER BY event_id LIMIT $1 FOR UPDATE SKIP LOCKED)`
	command, err := tx.Exec(ctx, query, append([]any{limit}, args...)...)
	if err != nil {
		return 0, fmt.Errorf("retry queue batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit retry batch: %w", err)
	}
	return int(command.RowsAffected()), nil
}

func selectorPredicate(selector QueueSelector, first int, retry bool) (string, []any, error) {
	return selectorPredicateQualified(selector, first, retry, "")
}

func selectorPredicateQualified(selector QueueSelector, first int, retry bool, qualifier string) (
	string,
	[]any,
	error,
) {
	if err := validateStatus(selector.Status); err != nil {
		return "", nil, err
	}
	if retry {
		if err := selector.ValidateForRetry(); err != nil {
			return "", nil, err
		}
	}
	var clauses []string
	var args []any
	next := first
	column := func(name string) string { return qualifier + name }
	if selector.ID != nil {
		clauses = append(clauses, fmt.Sprintf("%s=$%d", column("event_id"), next))
		args, next = append(args, *selector.ID), next+1
	}
	if selector.Listener != "" {
		clauses = append(clauses, fmt.Sprintf("%s=$%d", column("listener"), next))
		args, next = append(args, selector.Listener), next+1
	}
	if selector.Status != "" {
		clauses = append(clauses, fmt.Sprintf("%s=$%d", column("status"), next))
		args = append(args, selector.Status)
	}
	if len(clauses) == 0 {
		return "", nil, nil
	}
	return " " + strings.Join(clauses, " AND "), args, nil
}

// ValidateForRetry is what a retry selector must satisfy, exported so a caller can refuse before
// opening a connection instead of restating the rule. internal/cli's copy had already drifted --
// it skipped validateStatus, so the CLI admitted selectors RetryBatch then refused.
func (s QueueSelector) ValidateForRetry() error {
	if err := validateStatus(s.Status); err != nil {
		return err
	}
	if s.ID == nil && s.Status == "" && s.Listener == "" {
		return fmt.Errorf("retry requires a selector")
	}
	if s.ID != nil && (s.Status != "" || s.Listener != "") {
		return fmt.Errorf("id selector cannot be combined with listener or status")
	}
	if s.ID == nil && (s.Status == "" || s.Listener == "") {
		return fmt.Errorf("listener retry requires both listener and status")
	}
	return nil
}

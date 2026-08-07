package reconcile

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Validator is this package's implementation of config's database-validation port. It reports
// every reachable catalog defect together; only unavailable infrastructure and invalid
// cross-package requests use error.
type Validator struct {
	pool *pgxpool.Pool
	cfg  config.Config
}

var _ config.DBValidator = (*Validator)(nil)

func NewValidator(pool *pgxpool.Pool, cfg config.Config) *Validator {
	return &Validator{pool: pool, cfg: cfg}
}

func (validator *Validator) Validate(ctx context.Context, checks []config.DeferredCheck) (config.Errors, error) {
	if err := validateDeferredKinds(checks); err != nil {
		return nil, err
	}
	if len(checks) == 0 {
		return nil, nil
	}
	if validator.pool == nil {
		return nil, errors.New("database validator has no pool")
	}
	tx, err := validator.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin deferred validation: %w", err)
	}
	defer tx.Rollback(context.Background())
	catalog := NewCatalog(tx)
	var diagnostics config.Errors
	for _, check := range checks {
		diagnostic, reported, err := validator.validateCheck(ctx, tx, catalog, check)
		if err != nil {
			return nil, err
		}
		if reported {
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	return diagnostics, nil
}

func validateDeferredKinds(checks []config.DeferredCheck) error {
	for _, check := range checks {
		if _, known := deferredRules[check.Kind]; !known {
			return errors.New(unknownDeferredKindRefusal(string(check.Kind)).Message())
		}
	}
	return nil
}

func (validator *Validator) validateCheck(
	ctx context.Context,
	tx pgx.Tx,
	catalog Catalog,
	check config.DeferredCheck,
) (config.Error, bool, error) {
	switch check.Kind {
	case config.TableExists:
		return checkTableExists(ctx, catalog, check)
	case config.ColumnsExist:
		return checkColumnsExist(ctx, catalog, check)
	case config.PrimaryKeyPresent:
		return checkPrimaryKeyPresent(ctx, catalog, check)
	case config.WhenParses:
		return validator.validateWhen(ctx, tx, check)
	default:
		return config.Error{}, false, errors.New(unknownDeferredKindRefusal(string(check.Kind)).Message())
	}
}

// validateWhen contains Parse/Describe and its catalog read in a savepoint. PostgreSQL marks a
// transaction failed after an invalid clause; rolling this read-only child back restores the outer
// transaction so the remaining deferred checks still report their diagnostics together.
func (validator *Validator) validateWhen(ctx context.Context, outer pgx.Tx, check config.DeferredCheck) (
	config.Error,
	bool,
	error,
) {
	child, err := outer.Begin(ctx)
	if err != nil {
		return config.Error{}, false, fmt.Errorf("begin when analysis savepoint: %w", err)
	}
	diagnostic, reported, checkErr := checkWhenParses(ctx, child, NewCatalog(child), check)
	rollbackErr := child.Rollback(ctx)
	if checkErr != nil {
		return config.Error{}, false, checkErr
	}
	if rollbackErr != nil {
		return config.Error{}, false, fmt.Errorf("rollback when analysis savepoint: %w", rollbackErr)
	}
	return diagnostic, reported, nil
}

func deferredDiagnostic(check config.DeferredCheck, message string) config.Error {
	return config.NewError(deferredRules[check.Kind], positionedCheck{check: check}, message)
}

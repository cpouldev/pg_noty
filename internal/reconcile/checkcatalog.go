package reconcile

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
)

func checkTableExists(ctx context.Context, catalog Catalog, check config.DeferredCheck) (config.Error, bool, error) {
	_, found, name, err := resolveDeferredTarget(ctx, catalog, check)
	if err != nil || found {
		return config.Error{}, false, err
	}
	return deferredDiagnostic(check, "target table "+name+" does not exist"), true, nil
}

func checkColumnsExist(ctx context.Context, catalog Catalog, check config.DeferredCheck) (config.Error, bool, error) {
	target, found, name, err := resolveDeferredTarget(ctx, catalog, check)
	if err != nil {
		return config.Error{}, false, err
	}
	if !found {
		return deferredDiagnostic(check, "target table "+name+" does not exist"), true, nil
	}
	missing := missingTargetColumns(check.Columns, target.Columns)
	if len(missing) == 0 {
		return config.Error{}, false, nil
	}
	return deferredDiagnostic(
		check,
		"columns "+strings.Join(missing, ", ")+" do not exist on target table "+name,
	), true, nil
}

func checkPrimaryKeyPresent(ctx context.Context, catalog Catalog, check config.DeferredCheck) (
	config.Error,
	bool,
	error,
) {
	target, found, name, err := resolveDeferredTarget(ctx, catalog, check)
	if err != nil {
		return config.Error{}, false, err
	}
	if !found {
		return deferredDiagnostic(check, "target table "+name+" does not exist"), true, nil
	}
	if len(target.PrimaryKeyColumns) != 0 {
		return config.Error{}, false, nil
	}
	return deferredDiagnostic(check, "target table "+name+" has no primary key"), true, nil
}

func resolveDeferredTarget(ctx context.Context, catalog Catalog, check config.DeferredCheck) (
	TargetReading,
	bool,
	string,
	error,
) {
	name, fault := schema.Qualified(check.Schema, check.Table)
	if fault != schema.IdentifierOK {
		return TargetReading{}, false, "", fmt.Errorf("deferred target is unusable: %s", fault)
	}
	target, found, err := catalog.ResolveTarget(ctx, name)
	return target, found, name, err
}

func missingTargetColumns(want, actual []string) []string {
	present := make(map[string]struct{}, len(actual))
	for _, name := range actual {
		present[name] = struct{}{}
	}
	var missing []string
	for _, name := range want {
		if _, exists := present[name]; !exists {
			missing = append(missing, name)
		}
	}
	return missing
}

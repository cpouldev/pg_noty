package reconcile

import (
	"context"
	"errors"
	"fmt"

	"github.com/cpouldev/pg_noty/internal/schema"
)

// probeBootstrap identifies which required registry bootstrap object is absent without exposing a
// driver diagnostic to an operator who needs the bootstrap remediation instead.
func probeBootstrap(ctx context.Context, catalog Catalog, serviceSchema string) (*Refusal, error) {
	quotedSchema, fault := schema.Quoted(serviceSchema)
	if fault != schema.IdentifierOK {
		return nil, fmt.Errorf("bootstrap schema name is unusable: %s", fault)
	}
	exists, err := catalog.SchemaExists(ctx, serviceSchema)
	if err != nil {
		return nil, errors.New("cannot inspect bootstrap schema")
	}
	if !exists {
		refusal := absentBootstrapRefusal("schema " + quotedSchema)
		return &refusal, nil
	}
	for _, table := range []string{schema.TableListeners, schema.TableListenerTriggers} {
		if !declaredRegistryTable(table) {
			return nil, errors.New("registry bootstrap inventory is incomplete")
		}
		qualified, err := qualifiedRegistryTable(serviceSchema, table)
		if err != nil {
			return nil, err
		}
		if _, exists, err = catalog.ResolveTarget(ctx, qualified); err != nil {
			return nil, errors.New("cannot inspect bootstrap tables")
		}
		if !exists {
			refusal := absentBootstrapRefusal(qualified)
			return &refusal, nil
		}
	}
	return nil, nil
}

func declaredRegistryTable(name string) bool {
	for _, object := range schema.Objects {
		if object.Name == name && object.Kind == schema.KindTable {
			return true
		}
	}
	return false
}

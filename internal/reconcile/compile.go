package reconcile

import (
	"fmt"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/source"
)

type compiledListener struct {
	Target source.Target
	Sets   []source.ObjectSet
}

// compileListener consumes catalog metadata unchanged. source.Target has no validation guarantee;
// this is where the catalog's bytes enter internal/source, their only quoting authority.
func compileListener(instance, serviceSchema string, listener config.Listener, reading TargetReading) (
	compiledListener,
	error,
) {
	target := source.Target{Schema: reading.Schema, Table: reading.Table, PrimaryKeyColumns: reading.PrimaryKeyColumns}
	sets, err := source.Generate(
		source.Request{
			Instance: instance, ServiceSchema: serviceSchema, Listener: listener, Target: target,
		},
	)
	if err != nil {
		return compiledListener{}, fmt.Errorf("generate listener %q: %w", listener.Name, err)
	}
	return compiledListener{Target: target, Sets: sets}, nil
}

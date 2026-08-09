package config

import "context"

// DeferredKind identifies one catalog-dependent fact that static validation cannot decide.
type DeferredKind string

const (
	TableExists       DeferredKind = "table_exists"
	ColumnsExist      DeferredKind = "columns_exist"
	PrimaryKeyPresent DeferredKind = "primary_key_present"
	WhenParses        DeferredKind = "when_parses"
)

// DeferredCheck is one catalog-dependent validation request. File, Line, Col and Path preserve
// this package's token so internal/reconcile can return config diagnostics in the same rendered
// format.
type DeferredCheck struct {
	Listener  string
	Operation string
	Kind      DeferredKind
	Schema    string
	Table     string
	Columns   []string
	When      string
	File      string
	Line      int
	Col       int
	Path      string
}

// DBValidator is the internal/reconcile port for catalog-dependent validation. Implementations
// must run Validate before any DDL is issued. Config.DeferredChecks is the single choke point
// that skips listeners with enabled: false; static validation still checks those listeners in
// full.
//
// Kinds are data rather than methods so adding another check does not widen this interface.
// Validate returns this package's positioned diagnostics. Its error return is reserved for
// infrastructure failures such as a lost connection, not diagnostics.
type DBValidator interface {
	Validate(context.Context, []DeferredCheck) (Errors, error)
}

// NoopDBValidator is the database-free stand-in callers can use without internal/reconcile.
type NoopDBValidator struct{}

// Keep the seam's conformance in non-test code so its shape cannot drift unnoticed.
var _ DBValidator = NoopDBValidator{}

// Validate accepts every check without opening a database.
func (NoopDBValidator) Validate(context.Context, []DeferredCheck) (Errors, error) {
	return nil, nil
}

// DeferredChecks projects the catalog-dependent work for enabled listeners. Disabled listeners
// have already received full static validation; this method is the one place that suppresses their
// database work, preserving the distinct AC #25 and AC #26 responsibilities.
func (c Config) DeferredChecks() []DeferredCheck {
	var checks []DeferredCheck
	for _, listener := range c.Listeners {
		if !listener.Enabled {
			continue
		}

		checks = append(checks, deferredCheck(listener, "", TableExists,
			listener.Trigger.tablePosition))
		if listener.Trigger.Payload.Mode == payloadModeKeys {
			checks = append(checks, deferredCheck(listener, "", PrimaryKeyPresent,
				listener.Trigger.tablePosition))
		}
		for _, operation := range listener.Trigger.Operations {
			if len(operation.Columns) != 0 {
				check := deferredCheck(listener, operation.Kind, ColumnsExist,
					operation.columnsPosition)
				check.Columns = append([]string(nil), operation.Columns...)
				checks = append(checks, check)
			}
			if operation.When != "" {
				check := deferredCheck(listener, operation.Kind, WhenParses,
					operation.whenPosition)
				check.When = operation.When
				checks = append(checks, check)
			}
		}
	}
	return checks
}

func deferredCheck(listener Listener, operation string, kind DeferredKind, at Positioned) DeferredCheck {
	schema, table, _ := splitQualifiedTable(listener.Trigger.Table)
	check := DeferredCheck{
		Listener: listener.Name, Operation: operation, Kind: kind,
		Schema: schema, Table: table,
	}
	if at != nil {
		check.File, check.Line, check.Col, check.Path = at.File(), at.Line(), at.Col(), at.Path()
	}
	return check
}

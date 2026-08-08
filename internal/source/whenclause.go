package source

import (
	"strings"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
)

// This is the package's one raw-SQL interpolation site. At DDL time internal/reconcile installs the
// trigger as the pg_noty role inside a transaction spanning every listener; at row-write time the
// text runs inside every writing transaction on the target table; and
// internal/config's environment-variable interpolation extends that authority to whoever sets the
// referenced variable. The boundary is intentional: a SQL parser is outside the dependency ceiling.
// Skill Pattern 3 also matters here: pgx uses its simple multi-statement protocol for zero-bind
// calls, so internal/reconcile must issue a when-bearing CREATE TRIGGER as the
// sole statement of its own Exec.
func whenClause(operation config.Operation) (string, error) {
	distinct, err := distinctCondition(operation)
	if err != nil {
		return "", err
	}
	switch {
	case distinct != "" && operation.When != "":
		// Each condition gets its own parentheses before AND is introduced, preserving
		// the precedence of the author's raw expression.
		return " WHEN ((" + distinct + ") AND (" + operation.When + "))", nil
	case distinct != "":
		return " WHEN (" + distinct + ")", nil
	case operation.When == "":
		return "", nil
	default:
		// Parentheses provide operator precedence only; they are not a security control.
		return " WHEN (" + operation.When + ")", nil
	}
}

func distinctCondition(operation config.Operation) (string, error) {
	if operation.Kind != "update" || !operation.IsDistinct {
		return "", nil
	}
	if len(operation.Columns) == 0 {
		return "pg_catalog.to_jsonb(OLD) IS DISTINCT FROM pg_catalog.to_jsonb(NEW)", nil
	}
	oldColumns := make([]string, 0, len(operation.Columns))
	newColumns := make([]string, 0, len(operation.Columns))
	for _, column := range operation.Columns {
		quoted, reason := schema.Quoted(column)
		if reason != schema.IdentifierOK {
			return "", unusableIdentifierError{field: "update column", value: column, reason: reason}
		}
		oldColumns = append(oldColumns, "OLD."+quoted)
		newColumns = append(newColumns, "NEW."+quoted)
	}
	return "pg_catalog.to_jsonb(ROW(" + strings.Join(oldColumns, ", ") + ")) IS DISTINCT FROM " +
		"pg_catalog.to_jsonb(ROW(" + strings.Join(newColumns, ", ") + "))", nil
}

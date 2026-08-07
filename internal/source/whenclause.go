package source

import "github.com/cpouldev/pg_noty/internal/config"

// This is the package's one raw-SQL interpolation site. At DDL time internal/reconcile installs the
// trigger as the pg_noty role inside a transaction spanning every listener; at row-write time the
// text runs inside every writing transaction on the target table; and
// internal/config's environment-variable interpolation extends that authority to whoever sets the
// referenced variable. The boundary is intentional: a SQL parser is outside the dependency ceiling.
// Skill Pattern 3 also matters here: pgx uses its simple multi-statement protocol for zero-bind
// calls, so internal/reconcile must issue a when-bearing CREATE TRIGGER as the
// sole statement of its own Exec.
func whenClause(operation config.Operation) string {
	if operation.When == "" {
		return ""
	}
	// Parentheses provide operator precedence only; they are not a security control.
	return " WHEN (" + operation.When + ")"
}

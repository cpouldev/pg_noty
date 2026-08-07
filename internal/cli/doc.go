// Package cli is the command-line composition layer for pg_noty.
//
// # Reference: command execution
//
// Commands are registered by their own files and are executed through Execute.
// The inventory is closed: commandinventory_test.go pins both its membership and
// its size, and readmesurface_test.go pins the same list against README.md, so a
// new command joins three places or none.
//
// # Reference: logging
//
// The CLI owns the slog handler and rejects unknown level and format values.
//
// # Reference: exit status
//
// Reconciliation outcomes use reconcile.Verdict.ExitCode; the process entry point
// is the only place that terminates the process. Any error that is not an
// outcomeError maps to VerdictError.ExitCode, and Execute writes its message to
// stderr on the way out. An outcomeError is deliberately silent there, because it
// carries a verdict rather than a fault: a command returning one has already
// written whatever the operator needs to read.
//
// Three commands report status 2. plan reports it for a pending change, bootstrap
// for a schema behind the migrations this binary embeds, and run for a database
// whose triggers have drifted, so a supervisor sees 2 rather than 1 for a database
// that needs apply.
package cli

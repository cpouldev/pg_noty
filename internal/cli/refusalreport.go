package cli

import (
	"fmt"
	"io"

	"github.com/cpouldev/pg_noty/internal/reconcile"
)

// reportRefusals writes each refusal with the remediation it carries.
//
// The remediation is written because it was not: internal/reconcile populates one for every refusal
// it constructs and Refusal.Remediation had no caller outside its own package, so an operator whose
// service schema was absent was told "required bootstrap object is absent" and never told that
// `pg_noty bootstrap` is the thing that answers it. That mattered little while run bootstrapped on
// its own; with auto_reconcile defaulting to false it is the first message most operators meet.
//
// It is one function rather than a line at each call site so that apply and both of run's startup
// paths -- the one that reconciles and the one that only verifies -- cannot come to render a
// refusal in three spellings.
func reportRefusals(out io.Writer, refusals []reconcile.Refusal) {
	for _, refusal := range refusals {
		_, _ = fmt.Fprintln(out, refusal.Message())
		if remediation := refusal.Remediation(); remediation != "" {
			_, _ = fmt.Fprintf(out, "  remedy: %s\n", remediation)
		}
	}
}

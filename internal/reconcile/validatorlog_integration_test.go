//go:build integration

package reconcile

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func assertDiagnosticAt(t *testing.T, diagnostics config.Errors, rule config.RuleID, line, col int, text string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Rule == rule {
			if diagnostic.Line != line || diagnostic.Col != col || !strings.Contains(
				strings.ToLower(diagnostic.Msg),
				text,
			) {
				t.Fatalf("%s diagnostic = %#v, want %d:%d containing %q", rule, diagnostic, line, col, text)
			}
			return
		}
	}
	t.Fatalf("diagnostics %#v omit rule %s", diagnostics, rule)
}

// assertNoDDL requires the window it reads to hold something. Every caller has just run a validation
// that queried the catalog, so an empty log is a tracer that recorded nothing rather than a validator
// that issued nothing, and the DDL scan below would then be satisfied by any implementation at all.
func assertNoDDL(t *testing.T, statements []string) {
	t.Helper()
	if len(statements) == 0 {
		t.Fatal("the recorded statement window is empty, so this scan reads no validator work at all")
	}
	for _, statement := range statements {
		upper := strings.ToUpper(statement)
		if strings.Contains(upper, "CREATE ") || strings.Contains(upper, "DROP ") || strings.Contains(
			upper,
			"ALTER ",
		) || strings.Contains(upper, "COMMENT ") {
			t.Fatalf("validator issued DDL: %s", statement)
		}
	}
}

type validatorQueryLog struct{ statements []string }

func (log *validatorQueryLog) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	log.statements = append(log.statements, data.SQL+" "+fmt.Sprint(data.Args))
	return ctx
}

func (*validatorQueryLog) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (log *validatorQueryLog) clear() { log.statements = nil }

func recordedValidatorPool(t *testing.T, dsn string) (*pgxpool.Pool, *validatorQueryLog) {
	t.Helper()
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	log := &validatorQueryLog{}
	poolConfig.ConnConfig.Tracer = log
	pool, err := pgxpool.NewWithConfig(t.Context(), poolConfig)
	if err != nil {
		t.Fatalf("open recorded pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, log
}

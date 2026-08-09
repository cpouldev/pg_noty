//go:build integration

package source

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The two readers below are the package's single route to a stored object comment. They are shared
// because two suites read the same catalog for different claims and neither owns a second copy: one
// asks whether the *literal grammar* survived a quote and a backslash, and the other
// asks whether the marker's own *content* survived whole -- the long operation spelling, and a body
// longer than the identifier bound. One reader, two questions.

// functionComment reads what pg_description holds for one generated function, and fails when there
// is none: a missing comment and an empty one are different facts, and only the first means the
// COMMENT statement never took effect.
func functionComment(t *testing.T, pool *pgxpool.Pool, schemaName, function string) string {
	t.Helper()
	var stored string
	err := pool.QueryRow(t.Context(),
		"SELECT d.description FROM pg_description d JOIN pg_proc p ON p.oid=d.objoid "+
			"JOIN pg_namespace n ON n.oid=p.pronamespace "+
			"WHERE d.classoid='pg_proc'::regclass AND n.nspname=$1 AND p.proname=$2",
		schemaName, function).Scan(&stored)
	if err != nil {
		t.Fatalf("pg_description holds no comment for function %s.%s: %v", schemaName, function, err)
	}
	return stored
}

// triggerComment reads what pg_description holds for one generated trigger on one target table.
func triggerComment(t *testing.T, pool *pgxpool.Pool, targetSchema, targetTable, trigger string) string {
	t.Helper()
	var stored string
	err := pool.QueryRow(t.Context(),
		"SELECT d.description FROM pg_description d JOIN pg_trigger g ON g.oid=d.objoid "+
			"JOIN pg_class c ON c.oid=g.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace "+
			"WHERE d.classoid='pg_trigger'::regclass AND n.nspname=$1 AND c.relname=$2 AND g.tgname=$3",
		targetSchema, targetTable, trigger).Scan(&stored)
	if err != nil {
		t.Fatalf("pg_description holds no comment for trigger %s on %s.%s: %v",
			trigger, targetSchema, targetTable, err)
	}
	return stored
}

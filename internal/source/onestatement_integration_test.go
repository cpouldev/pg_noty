//go:build integration

package source

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// executedStatementCount runs one statement text the way internal/reconcile does and answers with
// how many statements the *server* ran.
//
// The mode matters more than the count. pgx selects the simple query protocol for any Exec carrying
// no bind arguments (github.com/jackc/pgx/v5@v5.10.0 conn.go:516-517), and generated DDL carries
// none -- so the one mode in which a second statement smuggled through an identifier would actually
// execute is the mode this package's DDL always travels, and the assertion has to be built there.
// Under the extended protocol the server refuses more than one statement per Parse, so a count taken
// there would answer the question by construction and say nothing about the path
// internal/reconcile uses.
//
// Under the simple protocol PostgreSQL replies with one CommandComplete per statement and pgconn
// surfaces one Result for each, so the number below is the server's own parse rather than a re-parse
// of the text. That is the whole point: inspecting the generated text cannot settle this, because
// the failure mode is a plausible-looking escape the server parses differently.
func executedStatementCount(ctx context.Context, pool *pgxpool.Pool, statement string) (int, error) {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer connection.Release()

	results, err := connection.Conn().PgConn().Exec(ctx, statement).ReadAll()
	return len(results), err
}

// execAsExactlyOneStatement executes one generated statement and refuses anything but one. Every
// integration case in this package reaches it through executeObjectSet, so "executes as exactly one
// statement" is asserted per executed statement rather than once per suite.
func execAsExactlyOneStatement(t *testing.T, pool *pgxpool.Pool, statement string) {
	t.Helper()
	ran, err := executedStatementCount(t.Context(), pool, statement)
	if err != nil {
		t.Fatalf("execute %s: %v", statement, err)
	}
	if ran != 1 {
		t.Fatalf("the server ran %d statements for one generated statement, want exactly 1; "+
			"a second statement executed:\n%s", ran, statement)
	}
}

// theStatementCountRows separate three answers that agree on every statement this generator emits
// today: the server's own parse, a counter wired to return 1, and a text-level scan for a semicolon.
// Without them nothing in the suite would notice any of the three being substituted for another.
var theStatementCountRows = []struct {
	name      string
	statement string
	want      int
}{
	{name: "one statement", statement: `CREATE TABLE public.counter_one (id int)`, want: 1},
	// The row a ';'-scanning substitute answers 2 for. A quoted identifier may hold a semicolon,
	// which is exactly why the injection class's table name carries one.
	{name: "a semicolon inside a quoted identifier", statement: `CREATE TABLE public."counter;two" (id int)`, want: 1},
	// The row separating "the text ends in ';'" from "a second statement ran".
	{name: "a trailing semicolon", statement: `CREATE TABLE public.counter_three (id int);`, want: 1},
	// B+1 for the pin at one: the row a counter returning 1 unconditionally answers wrongly.
	{name: "two statements", statement: `CREATE TABLE public.counter_four (id int); CREATE TABLE public.counter_five (id int)`, want: 2},
	{name: "three statements", statement: `CREATE TABLE public.counter_six (id int); CREATE TABLE public.counter_seven (id int); DROP TABLE public.counter_six`, want: 3},
}

// TestTheOneStatementCounterCountsWhatTheServerRan gives execAsExactlyOneStatement its own
// falsifiability. Every statement this generator emits passes the counter today, so the counter is
// the assertion no generated input can make fail -- and an assertion that cannot be made to fail is
// not evidence.
func TestTheOneStatementCounterCountsWhatTheServerRan(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	for _, row := range theStatementCountRows {
		t.Run(row.name, func(t *testing.T) {
			ran, err := executedStatementCount(t.Context(), pool, row.statement)
			if err != nil {
				t.Fatalf("execute %s: %v", row.statement, err)
			}
			if ran != row.want {
				t.Fatalf("the server ran %d statements, want %d:\n%s", ran, row.want, row.statement)
			}
		})
	}
}

//go:build integration

package source

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestUppercaseConfiguredColumnSelectsExactColumn(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.status_target ("Status" text, status text, id int)`)
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Target.Table = "status_target"
	request.Listener.Trigger.Payload = config.Payload{Mode: "columns", Columns: []string{"Status"}}
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])
	mustExecOn(t, pool, `INSERT INTO public.status_target ("Status", status, id) VALUES ('UP', 'low', 1)`)
	var payload string
	if err := pool.QueryRow(
		t.Context(),
		"SELECT payload::text FROM noty.events ORDER BY id DESC LIMIT 1",
	).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != `{"new": {"Status": "UP"}, "old": null}` && payload != `{"new":{"Status":"UP"},"old":null}` {
		t.Fatalf("payload selected the wrong spelling: %s", payload)
	}
	var unquoted string
	if err := pool.QueryRow(
		t.Context(),
		`SELECT status FROM public.status_target WHERE id=1`,
	).Scan(&unquoted); err != nil || unquoted != "low" {
		t.Fatalf("unquoted control did not select lowercase status: %q %v", unquoted, err)
	}
}

func TestCatalogPrimaryKeyQuoteAndExcludeSubtraction(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	primary := `a"b`
	primaryQuoted, _ := schema.Quoted(primary)
	mustExecOn(t, pool, "CREATE TABLE public.pk_target ("+primaryQuoted+" int PRIMARY KEY, other text)")
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Target.Table, request.Target.PrimaryKeyColumns = "pk_target", []string{primary}
	request.Listener.Trigger.Payload = config.Payload{Mode: "keys_only"}
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])
	mustExecOn(t, pool, "INSERT INTO public.pk_target ("+primaryQuoted+") VALUES (7)")
	var payload string
	if err := pool.QueryRow(
		t.Context(),
		"SELECT payload::text FROM noty.events ORDER BY id DESC LIMIT 1",
	).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != `{"new": {"a\"b": 7}, "old": null}` && payload != `{"new":{"a\"b":7},"old":null}` {
		t.Fatalf("primary key payload = %s", payload)
	}
}

func TestBoundaryExcludeNamesAreAbsentAndOtherColumnsRemain(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	columns := []string{`a"b`, "comma,name", "brace{}", `slash\name`}
	definitions := "id int"
	for _, column := range columns {
		quoted, fault := schema.Quoted(column)
		if fault != schema.IdentifierOK {
			t.Fatal(fault)
		}
		definitions += ", " + quoted + " text"
	}
	mustExecOn(t, pool, "CREATE TABLE public.exclude_target ("+definitions+")")
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Target.Table = "exclude_target"
	request.Listener.Trigger.Payload = config.Payload{Mode: "full", Exclude: columns}
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	executeObjectSet(t, pool, sets[0])
	mustExecOn(t, pool, "INSERT INTO public.exclude_target (id) VALUES (9)")
	var payload string
	if err := pool.QueryRow(
		t.Context(),
		"SELECT payload::text FROM noty.events ORDER BY id DESC LIMIT 1",
	).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if payload != `{"new": {"id": 9}, "old": null}` && payload != `{"new":{"id":9},"old":null}` {
		t.Fatalf("excluded payload = %s", payload)
	}
}

//go:build integration

package reconcile

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBootstrapProbeNamesEachAbsentStateAndAcceptsHealthyDatabase(t *testing.T) {
	skipIfShort(t)
	noSchema := freshDatabase(t)
	noSchemaRefusal, err := probeBootstrapInTransaction(t, noSchema)
	assertBootstrapRefusal(t, noSchemaRefusal, err, schemaSubject(t, harnessSchema))

	onlySchema := freshDatabase(t)
	service, _ := schema.Quoted(harnessSchema)
	mustExecOn(t, onlySchema, "CREATE SCHEMA "+service)
	listeners, _ := schema.Qualified(harnessSchema, schema.TableListeners)
	listenersRefusal, err := probeBootstrapInTransaction(t, onlySchema)
	assertBootstrapRefusal(t, listenersRefusal, err, listeners)

	onlyListeners := freshDatabase(t)
	mustExecOn(t, onlyListeners, "CREATE SCHEMA "+service)
	mustExecOn(t, onlyListeners, "CREATE TABLE "+listeners+" (name text PRIMARY KEY)")
	triggers, _ := schema.Qualified(harnessSchema, schema.TableListenerTriggers)
	triggersRefusal, err := probeBootstrapInTransaction(t, onlyListeners)
	assertBootstrapRefusal(t, triggersRefusal, err, triggers)

	listenersView := freshDatabase(t)
	mustExecOn(t, listenersView, "CREATE SCHEMA "+service)
	mustExecOn(t, listenersView, "CREATE VIEW "+listeners+" AS SELECT 'not-a-table'::text AS name")
	mustExecOn(t, listenersView, "CREATE TABLE "+triggers+" (listener text PRIMARY KEY)")
	viewRefusal, err := probeBootstrapInTransaction(t, listenersView)
	assertBootstrapRefusal(t, viewRefusal, err, listeners)

	healthy := freshDatabase(t)
	prepareOwnershipDatabase(t, healthy)
	if refusal, err := probeBootstrapInTransaction(t, healthy); err != nil || refusal != nil {
		t.Fatalf("healthy probe = %#v, %v; want no refusal", refusal, err)
	}
}

func probeBootstrapInTransaction(t *testing.T, pool *pgxpool.Pool) (*Refusal, error) {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	return probeBootstrap(t.Context(), NewCatalog(tx), harnessSchema)
}

func assertBootstrapRefusal(t *testing.T, got *Refusal, err error, subject string) {
	t.Helper()
	want := absentBootstrapRefusal(subject)
	if err != nil || got == nil || got.Message() != want.Message() || got.Remediation() != want.Remediation() {
		t.Fatalf("bootstrap probe = %#v, %v; want %q with remediation %q", got, err, want.Message(), want.Remediation())
	}
}

func schemaSubject(t *testing.T, name string) string {
	t.Helper()
	quoted, fault := schema.Quoted(name)
	if fault != schema.IdentifierOK {
		t.Fatalf("schema %q cannot be quoted: %s", name, fault)
	}
	return "schema " + quoted
}

//go:build integration

package source

import (
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

// theLiteralVariants are the two bytes deciding between quoteLiteral's two grammars: a quote doubles
// inside the plain form, and a backslash forces the E” form with both doubled. An identifier quoter
// applied to a literal produces text the server accepts and misreads, so every literal below is
// emitted, executed and read back rather than inspected.
//
// Route: generator boundary. internal/config refuses both bytes in a listener name and an instance
// through identifierDefect's charset rule, so these values are written straight into the Request;
// TestRoutingPinExaminesAllThreeRules is what fails first if that ceiling moves. The
// target table is catalog-sourced and needs no such guard.
var theLiteralVariants = []struct{ name, instance, listener, table string }{
	{name: "quote", instance: `instance'one`, listener: `listener'one`, table: `literal'one`},
	{name: "backslash", instance: `instance\one`, listener: `listener\one`, table: `literal\one`},
}

// installLiteralBearingTrigger creates the target and applies the generated objects for one variant,
// returning the object set so the read-backs compare against its own rendered marker rather than
// against a second spelling of it.
func installLiteralBearingTrigger(t *testing.T, pool *pgxpool.Pool, instance, listener, table string) ObjectSet {
	t.Helper()
	mustExecOn(t, pool, "CREATE TABLE public."+mustQuoteIdentifier(t, table)+" (id int)")
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Instance, request.Listener.Name, request.Target.Table = instance, listener, table
	sets, err := Generate(request)
	if err != nil || len(sets) != 1 {
		t.Fatalf("generating for %q produced %d object sets: %v", listener, len(sets), err)
	}
	executeObjectSet(t, pool, sets[0])
	return sets[0]
}

func TestGeneratedLiteralAndMarkerValuesRoundTrip(t *testing.T) {
	skipIfShort(t)
	for _, variant := range theLiteralVariants {
		t.Run(
			variant.name, func(t *testing.T) {
				pool := freshDatabase(t)
				applySourceMigrations(t, pool)
				installLiteralBearingTrigger(t, pool, variant.instance, variant.listener, variant.table)
				mustExecOn(t, pool, "INSERT INTO public."+mustQuoteIdentifier(t, variant.table)+" (id) VALUES (1)")

				var gotListener, gotTable, gotOperation string
				if err := pool.QueryRow(
					t.Context(),
					"SELECT listener, table_name, operation FROM noty.events ORDER BY id DESC LIMIT 1",
				).Scan(&gotListener, &gotTable, &gotOperation); err != nil {
					t.Fatal(err)
				}
				wantTable, fault := schema.Qualified("public", variant.table)
				if fault != schema.IdentifierOK {
					t.Fatal(fault)
				}
				if gotListener != variant.listener {
					t.Errorf("listener round-tripped to %q, want %q byte for byte", gotListener, variant.listener)
				}
				if gotTable != wantTable {
					t.Errorf("table_name round-tripped to %q, want %q byte for byte", gotTable, wantTable)
				}
				// The operation is a closed-vocabulary value, so its own bytes can never carry a quote
				// or a backslash: operationAbbreviationFor refuses everything outside the three. Its
				// round trip is asserted here, beside neighbours that do carry them, which is the
				// failure this literal actually has -- an escaping mistake in the literal before it
				// shifts the parse and the operation reads back as something else.
				if gotOperation != "insert" {
					t.Errorf(
						"operation round-tripped to %q beside a %s-bearing literal, want \"insert\"",
						gotOperation, variant.name,
					)
				}
			},
		)
	}
}

// TestTheGeneratedChannelAndMarkerRoundTripThroughTheServer is the other two literals. The channel
// is read off the notification the server delivered and the marker out of pg_description, because
// neither is observable in the event row the case above reads: a channel mis-escaped into a
// different name notifies nobody, and a mis-escaped marker is a comment internal/reconcile then
// fails to recognise as its own. This is the literal-grammar claim; the claim about the marker's
// *content* lives in markerroundtrip_integration_test.go.
func TestTheGeneratedChannelAndMarkerRoundTripThroughTheServer(t *testing.T) {
	skipIfShort(t)
	for _, variant := range theLiteralVariants {
		t.Run(
			variant.name, func(t *testing.T) {
				pool := freshDatabase(t)
				applySourceMigrations(t, pool)
				set := installLiteralBearingTrigger(t, pool, variant.instance, variant.listener, variant.table)
				listening := openNotificationConnection(t, harnessConfig(t), variant.instance)

				mustExecOn(t, pool, "INSERT INTO public."+mustQuoteIdentifier(t, variant.table)+" (id) VALUES (1)")

				wantChannel := "pg_noty_events_" + variant.instance
				delivered := nextNotification(t, listening, 2*time.Second)
				if delivered == nil {
					t.Fatalf(
						"no notification arrived on %q, so the channel literal did not survive the "+
							"emission it was quoted for", wantChannel,
					)
				}
				if delivered.Channel != wantChannel {
					t.Errorf(
						"the server delivered on channel %q, want %q byte for byte",
						delivered.Channel, wantChannel,
					)
				}

				// Both comments, because the two statements quote the same marker independently and a
				// mistake in one is invisible in the other.
				if got := functionComment(t, pool, harnessSchema, set.FunctionName); got != set.Marker {
					t.Errorf(
						"the function's stored comment is %q, want the emitted marker %q byte for byte",
						got, set.Marker,
					)
				}
				if got := triggerComment(t, pool, "public", variant.table, set.TriggerName); got != set.Marker {
					t.Errorf(
						"the trigger's stored comment is %q, want the emitted marker %q byte for byte",
						got, set.Marker,
					)
				}
			},
		)
	}
}

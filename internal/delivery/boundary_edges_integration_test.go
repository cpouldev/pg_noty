//go:build integration

package delivery

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
)

func TestHostileQualifiedIdentifiersRoundTripThroughLiveEnvelope(t *testing.T) {
	db := newDeliveryDatabase(t)
	customerSchema, customerTable := `cust.dot"x_`+db.cfg.Instance, `orders.dot"y`
	quotedSchema, fault := schema.Quoted(customerSchema)
	if fault != schema.IdentifierOK {
		t.Fatal(fault)
	}
	quotedTable, fault := schema.Quoted(customerTable)
	if fault != schema.IdentifierOK {
		t.Fatal(fault)
	}
	if _, err := db.pool.Exec(t.Context(), "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(t.Context(), "CREATE TABLE "+quotedSchema+"."+quotedTable+" (id int)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.pool.Exec(t.Context(), "DROP SCHEMA "+quotedSchema+" CASCADE") })
	qualified, fault := schema.Qualified(customerSchema, customerTable)
	if fault != schema.IdentifierOK {
		t.Fatal(fault)
	}
	events, queue := deliveryTable(t, db, schema.TableEvents), deliveryTable(t, db, schema.TableEventQueue)
	var event source.Event
	if err := db.pool.QueryRow(
		t.Context(),
		"INSERT INTO "+events+" (listener,table_name,operation,payload,txid,occurred_at) VALUES ('orders',$1,'insert','{}',pg_current_xact_id(),clock_timestamp()) RETURNING id,occurred_at,txid",
		qualified,
	).Scan(&event.ID, &event.OccurredAt, &event.TXID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(
		t.Context(),
		"INSERT INTO "+queue+" (event_id,occurred_at,listener,status,attempts,next_attempt_at) VALUES ($1,$2,'orders','pending',0,now())",
		event.ID,
		event.OccurredAt,
	); err != nil {
		t.Fatal(err)
	}
	event.Listener, event.Operation, event.Table, event.Payload = "orders", "insert", qualified, []byte(`{}`)
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK})
	worker := NewWorker(
		openDeliverySource(t, db, "hostile", time.Minute),
		nil,
		WorkerConfig{
			Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(), Client: http.DefaultClient, Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	if !worker.Dispatch(t.Context(), event) {
		t.Fatal("hostile event was not admitted")
	}
	worker.Wait()
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(fixture.Requests()[0].Body, &envelope); err != nil {
		t.Fatal(err)
	}
	var table struct{ Schema, Name string }
	if err := json.Unmarshal(envelope["table"], &table); err != nil {
		t.Fatal(err)
	}
	if table.Schema != customerSchema || table.Name != customerTable {
		t.Fatalf("table=%+v, want hostile names", table)
	}
}

func TestPayloadSizeEqualityCasesAreLive(t *testing.T) {
	db := newDeliveryDatabase(t)
	sizes := []int{7, 8, 9}
	events := make([]source.Event, len(sizes))
	for i, size := range sizes {
		events[i] = seedDeliveryEvent(t, db, "orders", fmt.Sprintf("%q", strings.Repeat("x", size-2)))
	}
	src := openDeliverySource(t, db, "payload-equality", time.Minute)
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK}, HTTPReply{Status: http.StatusOK})
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			BatchSize: 3, GlobalConcurrency: 3, Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(), Client: http.DefaultClient, MaxPayloadBytes: 8,
					Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	if len(fixture.Requests()) != 2 {
		t.Fatalf("requests=%d, want N-1 and N only", len(fixture.Requests()))
	}
	if present, _, _, _, _ := queueState(t, db, events[0].ID); present {
		t.Fatal("N-1 remained queued")
	}
	if present, _, _, _, _ := queueState(t, db, events[1].ID); present {
		t.Fatal("N remained queued")
	}
	if present, status, _, _, _ := queueState(t, db, events[2].ID); !present || status != "dead" {
		t.Fatalf("N+1 state present=%t status=%q", present, status)
	}
}

func noSecretMarker(marker string, surfaces ...string) error {
	for _, surface := range surfaces {
		if strings.Contains(surface, marker) {
			return fmt.Errorf("secret marker found")
		}
	}
	return nil
}

type headerEchoClient struct {
	terminalID int64
	mu         sync.Mutex
	sent       []string
}

func (c *headerEchoClient) Do(request *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(request.Body)
	var echoed strings.Builder
	for name, values := range request.Header {
		fmt.Fprintf(&echoed, "%s=%s\n", name, strings.Join(values, ","))
	}
	echoed.WriteString(strings.Repeat("response-padding", 300))
	c.mu.Lock()
	c.sent = append(c.sent, string(body)+fmt.Sprint(request.Header))
	c.mu.Unlock()
	status := http.StatusOK
	if id, _ := strconv.ParseInt(request.Header.Get("X-Pg-Noty-Event-Id"), 10, 64); id == c.terminalID {
		status = http.StatusBadRequest
	}
	return &http.Response{
		StatusCode: status, Body: io.NopCloser(strings.NewReader(echoed.String())), Header: make(http.Header),
	}, nil
}

func TestSecretMarkerOracleIsFalsifiableAndLive(t *testing.T) {
	marker := "secret-marker-31"
	if noSecretMarker(marker, "safe diagnostic") != nil || noSecretMarker(marker, "planted "+marker) == nil {
		t.Fatal("secret oracle is not falsifiable")
	}
	db := newDeliveryDatabase(t)
	event, terminal := seedDeliveryEvent(t, db, "orders", `{"id":32}`), seedDeliveryEvent(t, db, "orders", `{"id":33}`)
	src := openDeliverySource(t, db, "secret-oracle", time.Minute)
	declared := config.Signing{Secrets: []string{marker}}
	client := &headerEchoClient{terminalID: terminal.ID}
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			BatchSize: 2, GlobalConcurrency: 2, Listeners: []ListenerConfig{
				{
					Name: "orders", URL: "http://echo.invalid", Client: client, Signer: NewSigner(declared.Secrets),
					Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	var snippet, recordedError, deadReason string
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT coalesce(d.response_snippet,''),coalesce(d.error,''),coalesce(q.dead_reason,'') FROM "+deliveryTable(
			t,
			db,
			schema.TableDeliveries,
		)+" d LEFT JOIN "+deliveryTable(
			t,
			db,
			schema.TableEventQueue,
		)+" q ON q.event_id=d.event_id WHERE d.event_id=$1",
		event.ID,
	).Scan(&snippet, &recordedError, &deadReason); err != nil {
		t.Fatal(err)
	}
	var terminalReason string
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT coalesce(dead_reason,'') FROM "+deliveryTable(t, db, schema.TableEventQueue)+" WHERE event_id=$1",
		terminal.ID,
	).Scan(&terminalReason); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	sent := append([]string(nil), client.sent...)
	client.mu.Unlock()
	if err := noSecretMarker(
		marker,
		strings.Join(sent, "\n"),
		snippet,
		recordedError,
		deadReason,
		terminalReason,
	); err != nil {
		t.Fatal(err)
	}
}

//go:build integration

package delivery

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func deliveryCount(t *testing.T, db deliveryDB, id int64) int {
	t.Helper()
	var count int
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT count(*) FROM "+deliveryTable(t, db, schema.TableDeliveries)+" WHERE event_id=$1",
		id,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func TestEnvelopeHeadersSignatureAndBodyAreExact(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "order_paid", `{"z":null,"message":"quote \"ok\"","unicode":"π"}`)
	src := openDeliverySource(t, db, "worker", time.Minute)
	claimed, err := src.Claim(t.Context(), 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %#v, %v", claimed, err)
	}
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK, Echo: true})
	worker := NewWorker(
		src, src, WorkerConfig{
			Listeners: []ListenerConfig{
				{
					Name: "order_paid", URL: fixture.URL(),
					Signer: NewSigner([]string{"old-secret", "new-secret"}), Client: http.DefaultClient,
					Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	if !worker.Dispatch(t.Context(), claimed[0]) {
		t.Fatal("delivery was not admitted")
	}
	worker.Wait()
	requests := fixture.Requests()
	if len(requests) != 1 {
		t.Fatalf("requests=%d, want one", len(requests))
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(requests[0].Body, &envelope); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pg_noty", "id", "listener", "table", "op", "occurred_at", "txid", "data"} {
		if _, ok := envelope[name]; !ok || name == "pg_noty" && string(envelope[name]) != `"1"` || name == "table" && string(envelope[name]) != `{"schema":"public","name":"orders"}` {
			t.Errorf("missing envelope field %q", name)
		}
	}
	if len(envelope) != 8 {
		t.Fatalf("envelope fields=%d, want exactly eight", len(envelope))
	}
	if _, ok := envelope["attempt"]; ok || strings.Contains(string(requests[0].Body), "attempt") {
		t.Fatal("attempt leaked into the envelope body")
	}
	if got := string(envelope["data"]); got != string(claimed[0].Payload) {
		t.Fatalf("data=%s, want stored payload %s", got, claimed[0].Payload)
	}
	for _, name := range []string{
		"X-Pg-Noty-Event-Id", "X-Pg-Noty-Signature", "X-Pg-Noty-Timestamp", "X-Pg-Noty-Attempt",
	} {
		if requests[0].Header.Get(name) == "" {
			t.Errorf("missing required header %s", name)
		}
	}
	if got := requests[0].Header.Get("X-Pg-Noty-Attempt"); got != "1" {
		t.Errorf("attempt header=%q, want 1", got)
	}
	timestamp := parseHeaderInt(t, requests[0].Header.Get("X-Pg-Noty-Timestamp"))
	if !NewSigner([]string{"old-secret"}).Verify(
		timestamp,
		requests[0].Body,
		strings.Split(requests[0].Header.Get("X-Pg-Noty-Signature"), ",")[0],
	) {
		t.Fatal("first signing secret did not verify the raw body")
	}
	if !NewSigner([]string{"new-secret"}).Verify(
		timestamp,
		requests[0].Body,
		strings.Split(requests[0].Header.Get("X-Pg-Noty-Signature"), ",")[1],
	) {
		t.Fatal("rotated signing secret did not verify the raw body")
	}
	if present, status, _, _, _ := queueState(t, db, event.ID); present || status != "" {
		t.Fatalf("successful queue state present=%t status=%q, want deleted", present, status)
	}
	if got := deliveryCount(t, db, event.ID); got != 1 {
		t.Fatalf("delivery rows=%d, want one", got)
	}
}
func parseHeaderInt(t *testing.T, value string) int64 {
	t.Helper()
	var parsed int64
	if _, err := fmt.Sscan(value, &parsed); err != nil {
		t.Fatalf("header %q is not an integer: %v", value, err)
	}
	return parsed
}
func TestTerminalStatus400IsDeadOnTheFirstRequest(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":5}`)
	src := openDeliverySource(t, db, "worker", time.Minute)
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusBadRequest, Body: "bad"})
	worker := NewWorker(
		src, src, WorkerConfig{
			Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(),
					Client: http.DefaultClient,
					Policy: Policy{MaxAttempts: 5, InitialInterval: time.Second, MaxInterval: time.Minute},
				},
			},
		},
	)
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	if got := len(fixture.Requests()); got != 1 {
		t.Fatalf("requests=%d, want one terminal request", got)
	}
	present, status, attempts, _, _ := queueState(t, db, event.ID)
	if !present || status != "dead" || attempts != 1 {
		t.Fatalf("queue present=%t status=%q attempts=%d, want dead/1", present, status, attempts)
	}
	if got := deliveryCount(t, db, event.ID); got != 1 {
		t.Fatalf("delivery rows=%d, want one", got)
	}
}
func TestOversizedPayloadIsDeadWithoutHTTPAndStoredBytesRemainFull(t *testing.T) {
	db := newDeliveryDatabase(t)
	large := `{"x":"aaaaa"}`
	event := seedDeliveryEvent(t, db, "orders", large)
	src := openDeliverySource(t, db, "worker", time.Minute)
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK})
	worker := NewWorker(
		src, src, WorkerConfig{
			BatchSize: 1, Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(),
					MaxPayloadBytes: len(`{"x":"aaaa"}`), Client: http.DefaultClient, Policy: Policy{MaxAttempts: 1},
				},
			},
		},
	)
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	if len(fixture.Requests()) != 0 {
		t.Fatal("oversized event reached HTTP")
	}
	present, status, _, _, _ := queueState(t, db, event.ID)
	if !present || status != "dead" {
		t.Fatalf("queue present=%t status=%q, want dead", present, status)
	}
	var stored string
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT payload::text FROM "+deliveryTable(t, db, schema.TableEvents)+" WHERE id=$1",
		event.ID,
	).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, "aaaaa") {
		t.Fatalf("stored payload=%q lost the oversized value", stored)
	}
}

func TestStatusClassificationIsTotalAndFailClosed(t *testing.T) {
	for status := 100; status <= 599; status++ {
		outcome := ClassifyStatus(status)
		if outcome != OutcomeSuccess && outcome != OutcomeRetryable && outcome != OutcomeTerminal {
			t.Fatalf("status %d has unknown outcome %v", status, outcome)
		}
	}
	for _, status := range []int{199, 300, 407, 499, -1, 600} {
		if ClassifyStatus(status) != OutcomeTerminal {
			t.Errorf("status %d is not fail-closed terminal", status)
		}
	}
	for _, status := range []int{200, 299} {
		if ClassifyStatus(status) != OutcomeSuccess {
			t.Errorf("status %d is not success", status)
		}
	}
	for _, status := range []int{408, 429, 500, 599} {
		if ClassifyStatus(status) != OutcomeRetryable {
			t.Errorf("status %d is not retryable", status)
		}
	}
}

func TestTimeoutDNSAndAcceptCloseFailuresRecordNullStatus(t *testing.T) {
	timeoutServer := httptest.NewServer(
		http.HandlerFunc(
			func(
				http.ResponseWriter,
				*http.Request,
			) {
				time.Sleep(200 * time.Millisecond)
			},
		),
	)
	defer timeoutServer.Close()
	closeFixture := newHTTPFixture(t, HTTPReply{TransportError: true})
	cases := []struct {
		name, endpoint string
		client         HTTPDoer
	}{
		{"refused", "http://127.0.0.1:1", NewClient(TransportConfig{Timeout: 100 * time.Millisecond})},
		{
			"dns", "http://delivery-does-not-resolve.invalid",
			NewClient(TransportConfig{Timeout: 100 * time.Millisecond}),
		},
		{"timeout", timeoutServer.URL, &http.Client{Timeout: 20 * time.Millisecond}},
		{"accept-close", closeFixture.URL(), http.DefaultClient},
	}
	for _, tc := range cases {
		t.Run(
			tc.name, func(t *testing.T) {
				db := newDeliveryDatabase(t)
				event := seedDeliveryEvent(t, db, "orders", `{"id":16}`)
				src := openDeliverySource(t, db, "worker", time.Minute)
				worker := NewWorker(
					src,
					src,
					WorkerConfig{
						Listeners: []ListenerConfig{
							{
								Name: "orders", URL: tc.endpoint, Client: tc.client, Policy: Policy{MaxAttempts: 1},
							},
						},
					},
				)
				if _, err := worker.RunOnce(t.Context()); err != nil {
					t.Fatal(err)
				}
				worker.Wait()
				var status *int
				var recordedError *string
				if err := db.pool.QueryRow(
					t.Context(),
					"SELECT http_status,error FROM "+deliveryTable(t, db, schema.TableDeliveries)+" WHERE event_id=$1",
					event.ID,
				).Scan(&status, &recordedError); err != nil {
					t.Fatal(err)
				}
				if status != nil || recordedError == nil || *recordedError == "" {
					t.Fatalf("status=%v error=%v, want null/nonempty", status, recordedError)
				}
			},
		)
	}
}

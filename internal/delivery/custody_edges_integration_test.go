//go:build integration

package delivery

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/cpouldev/pg_noty/internal/source"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestCrashHelper is run only in the child process of TestKilledWorkerIsReclaimed.
func TestCrashHelper(t *testing.T) {
	dsn := os.Getenv("PGNOTY_CRASH_DSN")
	if dsn == "" {
		return
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cfg := config.Config{
		Instance: os.Getenv("PGNOTY_CRASH_INSTANCE"),
		Database: config.Database{URL: dsn, Schema: os.Getenv("PGNOTY_CRASH_SCHEMA")},
	}
	src, err := source.Open(t.Context(), pool, cfg, source.Options{LeasedBy: "crash-child", Lease: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	client := localListenerClient(time.Second)
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			Listeners: []ListenerConfig{
				{
					Name: "orders", URL: os.Getenv("PGNOTY_CRASH_URL"), Client: client, Policy: Policy{MaxAttempts: 2},
				},
			},
		},
	)
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
}

func TestKilledWorkerIsReclaimedAndRedelivered(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":30}`)
	hold := make(chan struct{})
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusOK, Hold: hold}, HTTPReply{Status: http.StatusOK})
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$", "-test.v")
	cmd.Env = append(
		os.Environ(),
		"PGNOTY_CRASH_DSN="+db.pool.Config().ConnConfig.ConnString(),
		"PGNOTY_CRASH_INSTANCE="+db.cfg.Instance,
		"PGNOTY_CRASH_SCHEMA="+db.cfg.Database.Schema,
		"PGNOTY_CRASH_URL="+fixture.URL(),
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(
		func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
		},
	)
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline) && len(fixture.Requests()) < 1; {
		time.Sleep(20 * time.Millisecond)
	}
	if len(fixture.Requests()) != 1 {
		t.Fatal("crash child did not reach an in-flight request")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	queue := deliveryTable(t, db, schema.TableEventQueue)
	if _, err := db.pool.Exec(
		t.Context(),
		"UPDATE "+queue+" SET leased_until=now()-interval '1 second' WHERE event_id=$1",
		event.ID,
	); err != nil {
		t.Fatal(err)
	}
	src := openDeliverySource(t, db, "replacement", time.Second)
	if swept, err := src.ReclaimExpired(t.Context()); err != nil || swept != 1 {
		t.Fatalf("reclaim=%d err=%v", swept, err)
	}
	claimed, err := src.Claim(t.Context(), 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("replacement claim=%#v err=%v", claimed, err)
	}
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(), Client: http.DefaultClient, Policy: Policy{MaxAttempts: 2},
				},
			},
		},
	)
	if !worker.Dispatch(t.Context(), claimed[0]) {
		t.Fatal("replacement delivery was not admitted")
	}
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline) && len(fixture.Requests()) < 2; {
		time.Sleep(10 * time.Millisecond)
	}
	close(hold)
	worker.Wait()
	if len(fixture.Requests()) != 2 || deliveryCount(t, db, event.ID) != 1 {
		t.Fatalf(
			"requests=%d deliveries=%d, want duplicate request and one recorded live attempt",
			len(fixture.Requests()),
			deliveryCount(t, db, event.ID),
		)
	}
	if present, status, attempts, _, _ := queueState(t, db, event.ID); present || status != "" || attempts != 0 {
		t.Fatalf("after replacement present=%t status=%q attempts=%d, want delivered", present, status, attempts)
	}
}

func TestRetryKeepsBodyAndRotatesTimestampAndSignature(t *testing.T) {
	db := newDeliveryDatabase(t)
	event := seedDeliveryEvent(t, db, "orders", `{"id":31}`)
	src := openDeliverySource(t, db, "retry-body", time.Minute)
	fixture := newHTTPFixture(t, HTTPReply{Status: http.StatusServiceUnavailable}, HTTPReply{Status: http.StatusOK})
	signer := NewSigner([]string{"old-secret", "new-secret"})
	policy := Policy{MaxAttempts: 2, Backoff: "fixed", InitialInterval: time.Millisecond, MaxInterval: time.Second}
	var ticks atomic.Int64
	base := time.Now()
	worker := NewWorker(
		src,
		src,
		WorkerConfig{
			Now: func() time.Time { return base.Add(time.Duration(ticks.Add(1)) * time.Second) },
			Listeners: []ListenerConfig{
				{
					Name: "orders", URL: fixture.URL(), Client: http.DefaultClient, Signer: signer, Policy: policy,
				},
			},
		},
	)
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	if _, err := db.pool.Exec(
		t.Context(),
		"UPDATE "+deliveryTable(t, db, schema.TableEventQueue)+" SET next_attempt_at=now() WHERE event_id=$1",
		event.ID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker.Wait()
	requests := fixture.Requests()
	if len(requests) != 2 || string(requests[0].Body) != string(requests[1].Body) || requests[0].Header.Get("X-Pg-Noty-Timestamp") == requests[1].Header.Get("X-Pg-Noty-Timestamp") || requests[0].Header.Get("X-Pg-Noty-Signature") == requests[1].Header.Get("X-Pg-Noty-Signature") {
		t.Fatalf("retry requests do not prove stable body and rotating timestamp/signature: %#v", requests)
	}
	ts0, _ := strconv.ParseInt(requests[0].Header.Get("X-Pg-Noty-Timestamp"), 10, 64)
	ts1, _ := strconv.ParseInt(requests[1].Header.Get("X-Pg-Noty-Timestamp"), 10, 64)
	sigs0, sigs1 := strings.Split(
		requests[0].Header.Get("X-Pg-Noty-Signature"),
		",",
	), strings.Split(requests[1].Header.Get("X-Pg-Noty-Signature"), ",")
	if len(sigs0) != 2 || len(sigs1) != 2 || !signer.Verify(ts0, requests[0].Body, sigs0[0]) || !signer.Verify(
		ts1,
		requests[1].Body,
		sigs1[1],
	) || signer.Verify(ts1, requests[0].Body, sigs0[0]) || signer.Verify(ts0, requests[1].Body, sigs1[1]) {
		t.Fatal("retry signatures did not verify only against their own timestamp and body")
	}
}

func TestDeadReleasesPartitionForRetention(t *testing.T) {
	db := newDeliveryDatabase(t)
	cfg := db.cfg
	cfg.Retention.PartitionInterval, cfg.Retention.Precreate, cfg.Retention.Keep = time.Hour, time.Hour, time.Hour
	events, queue := deliveryTable(t, db, schema.TableEvents), deliveryTable(t, db, schema.TableEventQueue)
	partition, fault := schema.Qualified(db.cfg.Database.Schema, "retention_old")
	if fault != schema.IdentifierOK {
		t.Fatal(fault)
	}
	to, from := time.Now().UTC().Add(-2*time.Hour), time.Now().UTC().Add(-3*time.Hour)
	if _, err := db.pool.Exec(
		t.Context(),
		fmt.Sprintf(
			"CREATE TABLE %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')",
			partition,
			events,
			from.Format(time.RFC3339Nano),
			to.Format(time.RFC3339Nano),
		),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(
		t.Context(),
		fmt.Sprintf("COMMENT ON TABLE %s IS 'pg_noty:v1:%s:partition'", partition, cfg.Instance),
	); err != nil {
		t.Fatal(err)
	}
	var eventID int64
	var occurred time.Time
	if err := db.pool.QueryRow(
		t.Context(),
		"INSERT INTO "+events+" (listener,table_name,operation,payload,txid,occurred_at) VALUES ('orders','\"public\".\"orders\"','insert','{}',pg_current_xact_id(),$1) RETURNING id,occurred_at",
		from.Add(time.Minute),
	).Scan(&eventID, &occurred); err != nil {
		t.Fatal(err)
	}
	if _, err := db.pool.Exec(
		t.Context(),
		"INSERT INTO "+queue+" (event_id,occurred_at,listener,status,attempts,next_attempt_at) VALUES ($1,$2,'orders','pending',0,now())",
		eventID,
		occurred,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := schema.ApplyRetention(t.Context(), db.pool, cfg, schema.Options{}); err == nil {
		t.Fatal("pending row did not refuse retention")
	}
	if _, err := db.pool.Exec(
		t.Context(),
		"UPDATE "+queue+" SET status='dead',dead_reason='terminal',leased_until=NULL,leased_by=NULL WHERE event_id=$1",
		eventID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := schema.ApplyRetention(t.Context(), db.pool, cfg, schema.Options{}); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := db.pool.QueryRow(
		t.Context(),
		"SELECT to_regclass($1) IS NOT NULL",
		db.cfg.Database.Schema+".retention_old",
	).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("dead row still blocked retention")
	}
}

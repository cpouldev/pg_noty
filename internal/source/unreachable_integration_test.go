//go:build integration

package source

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// atAnUnreachableEndpoint points a configuration at a port nothing listens on and leaves every other
// field alone. 127.0.0.1:1 refuses immediately rather than hanging, so the error a caller has to
// classify arrives as a connection failure and not as a deadline -- which would be indistinguishable
// from the cancellation class covered elsewhere.
func atAnUnreachableEndpoint(t *testing.T, cfg config.Config) config.Config {
	t.Helper()
	parsed, err := url.Parse(cfg.Database.URL)
	if err != nil {
		t.Fatalf("parse connection string: %v", err)
	}
	parsed.Host = "127.0.0.1:1"
	cfg.Database.URL = parsed.String()
	return cfg
}

// transitionCalls is the four methods behind one signature, so a classification claim is made about
// each of them rather than about whichever one a test happened to reach.
func transitionCalls(source *TriggerSource, event Event) []struct {
	name string
	call func(context.Context) error
} {
	delivery := Delivery{}
	return []struct {
		name string
		call func(context.Context) error
	}{
		{"claim", func(ctx context.Context) error { _, err := source.Claim(ctx, 1); return err }},
		{"ack", func(ctx context.Context) error { return source.Ack(ctx, event, delivery) }},
		{"nack", func(ctx context.Context) error { return source.Nack(ctx, event, delivery, time.Now()) }},
		{"dead", func(ctx context.Context) error { return source.Dead(ctx, event, delivery, "reason") }},
	}
}

// TestAnUnreachableDatabaseIsTellableApartFromTheSentinels is the error-classification clause.
// Each method is called twice with the same arguments -- once against the harness and once against a
// refused endpoint -- because "an unreachable database yields an error" is also true of a call that
// was simply late, and only the pair says a caller can tell the two apart.
func TestAnUnreachableDatabaseIsTellableApartFromTheSentinels(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	reachable, err := Open(t.Context(), pool, harnessConfig(t), Options{LeasedBy: "worker", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer reachable.Close()

	unreachableConfig := atAnUnreachableEndpoint(t, harnessConfig(t))
	unreachablePool, err := pgxpool.New(t.Context(), unreachableConfig.Database.URL)
	if err != nil {
		t.Fatalf("build a pool for the refused endpoint: %v", err)
	}
	t.Cleanup(unreachablePool.Close)
	unreachable, err := Open(
		t.Context(),
		unreachablePool,
		unreachableConfig,
		Options{LeasedBy: "worker", Lease: time.Minute},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer unreachable.Close()

	// An event that was never claimed, so every transition against the reachable database answers
	// with the very sentinel the unreachable one must not be confused for.
	absent := Event{ID: 999_999, Attempt: 1}
	for index, against := range transitionCalls(reachable, absent) {
		t.Run(
			against.name, func(t *testing.T) {
				assertLateCallAnswersItsSentinel(t, against.name, against.call(t.Context()))

				faulted := transitionCalls(unreachable, absent)[index].call(t.Context())
				assertUnreachableIsClassifiable(t, against.name, faulted)
			},
		)
	}
}

// assertLateCallAnswersItsSentinel is the near-miss half. Claim has no sentinel for an empty queue --
// it answers empty and nil by design -- so its row is that answer rather than an error.
func assertLateCallAnswersItsSentinel(t *testing.T, name string, err error) {
	t.Helper()
	if name == "claim" {
		if err != nil {
			t.Fatalf("claim against the reachable database returned %v, want the empty-and-nil answer", err)
		}
		return
	}
	if !errors.Is(err, ErrEventNotClaimed) {
		t.Fatalf(
			"%s against the reachable database returned %v, want ErrEventNotClaimed; without "+
				"this the case below cannot show the two are tellable apart", name, err,
		)
	}
}

func assertUnreachableIsClassifiable(t *testing.T, name string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s against a refused endpoint returned no error", name)
	}
	for _, confusable := range []struct {
		label string
		with  error
	}{
		{"ErrEventNotClaimed", ErrEventNotClaimed},
		{"ErrSourceClosed", ErrSourceClosed},
		{"context.Canceled", context.Canceled},
		{"context.DeadlineExceeded", context.DeadlineExceeded},
	} {
		if errors.Is(err, confusable.with) {
			t.Errorf(
				"%s against a refused endpoint returned %v, which a caller reads as %s",
				name, err, confusable.label,
			)
		}
	}
	// The positive half: a caller routing on the classes above falls through to "the database is
	// unreachable", and this is the evidence for that fall-through rather than for the absence of
	// the others.
	var network *net.OpError
	if !errors.As(err, &network) {
		t.Errorf(
			"%s against a refused endpoint returned %v, which carries no network error a "+
				"caller could classify as an infrastructure fault", name, err,
		)
	}
}

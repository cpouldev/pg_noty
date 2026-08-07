//go:build integration

package source

import (
	"net"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The second listening-loss case, and listen.go's other once-only latch: an endpoint that refuses for
// the whole run while the ordinary connection still works -- a proxy that never supported LISTEN,
// or a session-mode port that is simply not there.
//
// The endpoint is a real socket that accepts and closes, because a refused connection cannot be
// counted and the whole claim here is about *how often* the source tries. Both sides of that are
// asserted: fewer attempts than a spin would make, and more than one, so a source that gave up
// after the first failure fails too.

const (
	// theRefusalWindow is how long the endpoint is watched. It spans several rungs of the ladder, so
	// the count separates a growing delay from a fixed short retry.
	theRefusalWindow = time.Second
	// theSocketSettlingTime is how long one failed connection attempt is watched before its sockets
	// are counted. A failure that opens more than one opens them microseconds apart.
	theSocketSettlingTime = 200 * time.Millisecond
)

// connectionsPerAttempt is how many sockets the driver opens for one failed connection attempt.
//
// Measured against an endpoint of its own rather than assumed: on pgx v5.10 a mid-startup hangup
// costs two sockets, and the driver promises nothing about that number. The bound below is written
// in attempts, which is what the bound counts, and converted with this reading -- so a driver
// that changes its mind moves the conversion instead of loosening the bound in silence.
func connectionsPerAttempt(t *testing.T, cfg config.Config) int {
	t.Helper()
	address, accepted := aRefusingListenEndpoint(t)
	connection, err := pgx.Connect(t.Context(), listeningAt(t, cfg, address).Database.ListenURL)
	if err == nil {
		_ = connection.Close(t.Context())
		t.Fatal("an endpoint that speaks no protocol completed a connection")
	}
	time.Sleep(theSocketSettlingTime)
	sockets := accepted()
	if sockets < 1 {
		t.Fatal(
			"a failed connection attempt opened no socket on the endpoint the source is " +
				"configured to listen at, so nothing below counts the source's attempts",
		)
	}
	return sockets
}

// aRefusingListenEndpoint is a socket that accepts a connection and closes it without speaking the
// protocol, and a reader of how many it has accepted.
func aRefusingListenEndpoint(t *testing.T) (string, func() int) {
	t.Helper()
	endpoint, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = endpoint.Close() })
	var accepted atomic.Int64
	go func() {
		for {
			connection, err := endpoint.Accept()
			if err != nil {
				return
			}
			accepted.Add(1)
			_ = connection.Close()
		}
	}()
	return endpoint.Addr().String(), func() int { return int(accepted.Load()) }
}

// listeningAt is the harness configuration with its listening endpoint moved and everything else --
// credentials, database, service schema -- left as it was, so the ordinary connection keeps working.
func listeningAt(t *testing.T, cfg config.Config, address string) config.Config {
	t.Helper()
	parsed, err := url.Parse(cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Host = address
	cfg.Database.ListenURL = parsed.String()
	return cfg
}

func TestAListeningEndpointRefusingForTheWholeRunIsRetriedWithADelay(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	perAttempt := connectionsPerAttempt(t, harnessConfig(t))
	address, accepted := aRefusingListenEndpoint(t)
	log := newListenLog()

	opened := time.Now()
	source := openListeningSource(t, pool, listeningAt(t, harnessConfig(t), address), log)
	log.await(t, theListenUnavailableMessage, 1, 5*time.Second)
	time.Sleep(time.Until(opened.Add(theRefusalWindow)))

	sockets := accepted()
	attempts := sockets / perAttempt
	t.Logf(
		"the source opened %d sockets in %s at %d per attempt: %d connection attempts",
		sockets, theRefusalWindow, perAttempt, attempts,
	)
	// One rung more than the ladder allows, for the scheduling slack between a timer firing and the
	// socket being accepted.
	if bound := attemptsWithin(theRefusalWindow) + 1; attempts > bound {
		t.Errorf(
			"the source made %d connection attempts in %s, and its own ladder allows at most "+
				"%d: a refusing endpoint is being spun on rather than retried with a delay",
			attempts, theRefusalWindow, bound,
		)
	}
	if attempts < 2 {
		t.Errorf(
			"the source made %d connection attempts in %s, so it stopped retrying and one blip "+
				"costs the process its latency optimisation permanently", attempts, theRefusalWindow,
		)
	}

	assertWakeChannelStillOpen(t, source)
	assertTheOtherFourStillServe(t, pool, source)
	if got := log.count(theListenUnavailableMessage); got != 1 {
		t.Errorf(
			"an endpoint that never answered was reported %d times over %d attempts, want "+
				"once: the operator is told that polling is the only path, not told it per retry",
			got, attempts,
		)
	}
	if got := log.count(theListenConnectedMessage); got != 0 {
		t.Errorf(
			"the source reported %s %d times against an endpoint that speaks no protocol",
			theListenConnectedMessage, got,
		)
	}
	assertNoWakeUpArrives(t, pool, source)
}

// assertNoWakeUpArrives is the near miss for the recovery clause: with the listening endpoint dead
// for the whole run there is nothing to re-establish, so a wake-up here would mean the source found
// its way onto the ordinary connection and the two URLs are not being kept apart.
func assertNoWakeUpArrives(t *testing.T, pool *pgxpool.Pool, source *TriggerSource) {
	t.Helper()
	if awaitWake(t, pool, source, 250*time.Millisecond) {
		t.Error(
			"a wake-up arrived while the configured listening endpoint was refusing, so " +
				"database.listen_url is not the connection the listener uses",
		)
	}
}

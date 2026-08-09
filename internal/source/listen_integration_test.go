//go:build integration

package source

import (
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// theListenURLApplicationName is how C41a's two cases are told apart. Pointing listen_url at the
// same string as the pool's URL makes the cases identical, so deleting listen.go's ListenURL branch
// passes both; naming the dedicated connection is a property only the configured URL can carry, and
// pg_stat_activity reports it back.
const theListenURLApplicationName = "pg_noty_listen_url_case"

func withApplicationName(t *testing.T, dsn, name string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse the harness connection string: %v", err)
	}
	query := parsed.Query()
	query.Set("application_name", name)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func TestDedicatedListenerUsesConfiguredURLAndOneBackend(t *testing.T) {
	skipIfShort(t)
	for _, useListenURL := range []bool{false, true} {
		t.Run(map[bool]string{false: "database_url", true: "listen_url"}[useListenURL], func(t *testing.T) {
			pool := freshDatabase(t)
			cfg := harnessConfig(t)
			if useListenURL {
				cfg.Database.ListenURL = withApplicationName(t, cfg.Database.URL, theListenURLApplicationName)
			}
			before := clientBackendCount(t, pool)
			source, err := Open(t.Context(), pool, cfg, Options{Listen: true})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = source.Close() })
			waitForDedicatedListener(t, pool, source)
			if got := clientBackendCount(t, pool); got != before+1 {
				t.Fatalf("running listener backends = %d, want %d", got, before+1)
			}
			assertTheListenerConnectedThroughTheConfiguredURL(t, pool, useListenURL)
			if err := source.Close(); err != nil {
				t.Fatal(err)
			}
			waitForBackendCount(t, pool, before)
		})
	}
}

// assertTheListenerConnectedThroughTheConfiguredURL reads which string the dedicated connection was
// opened from. Both directions are asserted -- listen_url must be used when it is set and must not
// be reached for when it is not -- so neither deleting the branch nor always taking it survives.
func assertTheListenerConnectedThroughTheConfiguredURL(t *testing.T, pool *pgxpool.Pool, useListenURL bool) {
	t.Helper()
	want := 0
	if useListenURL {
		want = 1
	}

	var named int
	err := pool.QueryRow(t.Context(), "SELECT count(*) FROM pg_stat_activity WHERE "+
		"datname = current_database() AND application_name = $1", theListenURLApplicationName).Scan(&named)
	if err != nil {
		t.Fatalf("read the listening backend's application_name: %v", err)
	}
	if named != want {
		t.Errorf("%d backends were opened from the listen_url, want %d; the dedicated connection "+
			"does not come from the URL this case configured", named, want)
	}
}

func waitForDedicatedListener(t *testing.T, pool *pgxpool.Pool, source *TriggerSource) {
	t.Helper()
	for attempt := 0; attempt < 40; attempt++ {
		mustExecOn(t, pool, `SELECT pg_notify('pg_noty_events_noty', '')`)
		select {
		case <-source.Notify():
			return
		case <-time.After(25 * time.Millisecond):
		}
	}
	t.Fatal("dedicated listener did not receive a notification")
}

func clientBackendCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND backend_type='client backend'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func waitForBackendCount(t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()
	for attempt := 0; attempt < 40; attempt++ {
		if clientBackendCount(t, pool) == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("backend count did not reach %d", want)
}

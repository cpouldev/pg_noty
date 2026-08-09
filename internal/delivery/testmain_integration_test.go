//go:build integration

package delivery

import (
	"context"
	"flag"
	"fmt"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const deliveryHarnessImage = "postgres:17.10-alpine"

var (
	deliveryContainer        *postgres.PostgresContainer
	deliveryDatabase         string
	deliveryStarted          atomic.Int64
	deliveryContainerRequest any
)

func TestMain(m *testing.M) { os.Exit(runDeliveryHarness(m)) }

func runDeliveryHarness(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}
	ctx := context.Background()
	container, err := startDeliveryContainer(ctx)
	if container != nil {
		defer container.Terminate(ctx)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "delivery harness unavailable: %v\n", err)
		return 1
	}
	deliveryContainer = container
	code := m.Run()
	if code == 0 && deliveryStarted.Load() != 1 {
		fmt.Fprintf(os.Stderr, "delivery harness started %d containers, want one\n", deliveryStarted.Load())
		return 1
	}
	return code
}

func startDeliveryContainer(ctx context.Context) (*postgres.PostgresContainer, error) {
	deliveryStarted.Add(1)
	deliveryDatabase = fmt.Sprintf("delivery_%d_%d", os.Getpid(), time.Now().UnixNano())
	return postgres.Run(ctx, deliveryHarnessImage,
		postgres.WithDatabase(deliveryDatabase), postgres.WithUsername("noty"),
		postgres.WithPassword("noty-harness-secret"), postgres.WithSQLDriver("pgx"),
		recordingDeliveryRequest(postgres.BasicWaitStrategies()))
}

func recordingDeliveryRequest[Option any](option Option) Option {
	recorded := reflect.MakeFunc(reflect.TypeOf(option), func(arguments []reflect.Value) []reflect.Value {
		deliveryContainerRequest = arguments[0].Interface()
		return reflect.ValueOf(option).Call(arguments)
	})
	return recorded.Interface().(Option)
}

func deliveryPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if deliveryContainer == nil {
		t.Skip("delivery harness is unavailable")
	}
	dsn, err := deliveryContainer.ConnectionString(t.Context(), "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

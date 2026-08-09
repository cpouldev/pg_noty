//go:build integration

package cli

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/delivery"
)

func TestRunDrainIntegrationHarnessIsPresent(t *testing.T) {
	t.Run(
		"clean", func(t *testing.T) {
			source := &cliDrainSource{event: cliDrainEvent()}
			worker := delivery.NewWorker(
				source, nil, delivery.WorkerConfig{
					Listeners: []delivery.ListenerConfig{
						{
							Name: "orders", Client: cliDrainClient{}, Policy: delivery.Policy{MaxAttempts: 1},
						},
					},
				},
			)
			if !worker.Dispatch(t.Context(), source.event) {
				t.Fatal("clean delivery was not admitted")
			}
			worker.Wait()
			if err := drainAndReport(
				t.Context(),
				runDependencies{worker: worker, logger: slogTestLogger()},
			); err != nil {
				t.Fatalf("clean drain=%v", err)
			}
		},
	)
	t.Run(
		"timeout", func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			source := &cliDrainSource{event: cliDrainEvent()}
			worker := delivery.NewWorker(
				source, nil, delivery.WorkerConfig{
					Listeners: []delivery.ListenerConfig{
						{
							Name: "orders", Client: cliBlockingClient{started: started, release: release},
							Policy: delivery.Policy{MaxAttempts: 1},
						},
					},
				},
			)
			if !worker.Dispatch(t.Context(), source.event) {
				t.Fatal("timed delivery was not admitted")
			}
			<-started
			deps := runDependencies{
				worker: worker, cfg: config.Config{Worker: config.Worker{DrainTimeout: 10 * time.Millisecond}},
				logger: slogTestLogger(),
			}
			if err := drainAndReport(t.Context(), deps); exitStatusOf(err) != 1 {
				t.Fatalf("timeout drain error=%v status=%d, want verdict error", err, exitStatusOf(err))
			}
			close(release)
			worker.Wait()
		},
	)
}

func cliDrainEvent() delivery.Event {
	return delivery.Event{ID: 91, Listener: "orders", Table: `"public"."orders"`, Payload: []byte(`{}`), Attempt: 1}
}

type cliDrainClient struct{}

func (cliDrainClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header),
	}, nil
}

type cliBlockingClient struct{ started, release chan struct{} }

func (client cliBlockingClient) Do(*http.Request) (*http.Response, error) {
	close(client.started)
	<-client.release
	return &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header),
	}, nil
}

type cliDrainSource struct {
	event delivery.Event
	nacks int
}

func (source *cliDrainSource) Claim(context.Context, int) ([]delivery.Event, error) { return nil, nil }
func (source *cliDrainSource) Ack(context.Context, delivery.Event, delivery.Delivery) error {
	return nil
}
func (source *cliDrainSource) Nack(context.Context, delivery.Event, delivery.Delivery, time.Time) error {
	source.nacks++
	return nil
}
func (source *cliDrainSource) Dead(context.Context, delivery.Event, delivery.Delivery, string) error {
	return nil
}
func (source *cliDrainSource) Notify() <-chan struct{} { return nil }
func (source *cliDrainSource) Close() error            { return nil }

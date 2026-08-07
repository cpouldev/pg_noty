package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
)

type runServer struct {
	server   *http.Server
	listener net.Listener
}

func bindRunServer(address string, health http.Handler, ready http.Handler, metrics http.Handler) (*runServer, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("bind run listener: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/healthz", health)
	mux.Handle("/readyz", ready)
	mux.Handle("/metrics", metrics)
	return &runServer{server: &http.Server{Handler: mux}, listener: listener}, nil
}

func (running *runServer) serve(logger *slog.Logger) {
	if err := running.server.Serve(running.listener); err != nil && err != http.ErrServerClosed && logger != nil {
		logger.Error("run server stopped", "error", err)
	}
}

func (running *runServer) shutdown(ctx context.Context) error {
	if running == nil || running.server == nil {
		return nil
	}
	return running.server.Shutdown(ctx)
}

func stopRunServer(running *runServer) {
	if running != nil {
		_ = running.shutdown(context.Background())
	}
}

package source

import (
	"context"
	"fmt"
	"time"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5"
)

const (
	listenInitialBackoff = 20 * time.Millisecond
	listenMaximumBackoff = 500 * time.Millisecond
)

func listenChannelName(instance string) (string, error) {
	channel, fault := schema.Quoted("pg_noty_events_" + instance)
	if fault != schema.IdentifierOK {
		return "", fmt.Errorf("listen channel is unusable: %s", fault)
	}
	return channel, nil
}

func (s *TriggerSource) listenURL() string {
	if s.cfg.Database.ListenURL != "" {
		return s.cfg.Database.ListenURL
	}
	return s.cfg.Database.URL
}

func (s *TriggerSource) listenLoop(ctx context.Context, channel string) {
	defer close(s.listenerDone)
	backoff, reported := listenInitialBackoff, false
	for {
		if ctx.Err() != nil {
			return
		}
		connection, err := pgx.Connect(ctx, s.listenURL())
		if err != nil {
			if !reported {
				s.reportListen("listen unavailable", err)
				reported = true
			}
			if !waitListen(ctx, backoff) {
				return
			}
			backoff = nextListenBackoff(backoff)
			continue
		}
		s.reportListen("listen connected", nil)
		backoff, reported = listenInitialBackoff, false
		err = receiveNotifications(ctx, connection, channel, s.signalWake)
		_ = connection.Close(context.Background())
		if ctx.Err() != nil {
			return
		}
		if !reported {
			s.reportListen("listen connection lost", err)
			reported = true
		}
		if !waitListen(ctx, backoff) {
			return
		}
		backoff = nextListenBackoff(backoff)
	}
}

func receiveNotifications(ctx context.Context, connection *pgx.Conn, channel string, signal func()) error {
	if _, err := connection.Exec(ctx, "LISTEN "+channel); err != nil {
		return err
	}
	for {
		if _, err := connection.WaitForNotification(ctx); err != nil {
			return err
		}
		signal()
	}
}

func waitListen(ctx context.Context, backoff time.Duration) bool {
	timer := time.NewTimer(backoff)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func nextListenBackoff(backoff time.Duration) time.Duration {
	next := backoff * 2
	if next > listenMaximumBackoff {
		return listenMaximumBackoff
	}
	return next
}

func (s *TriggerSource) reportListen(message string, err error) {
	if s.opts.Logger == nil {
		return
	}
	if err == nil {
		s.opts.Logger.Info(message)
		return
	}
	s.opts.Logger.Warn(message, "error", err)
}

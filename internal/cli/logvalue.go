package cli

import (
	"log/slog"
	"net/url"
	"slices"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/delivery"
)

type listenerLogValue struct {
	Name        string
	Method      string
	Destination string
	HeaderNames []string
}

func (value listenerLogValue) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("name", value.Name),
		slog.String("method", value.Method),
		slog.String("destination", value.Destination),
		slog.Any("header_names", value.HeaderNames),
	)
}

type dispatchLogValue struct {
	EventID  int64
	Listener string
	Status   int
	Error    string
}

func (value dispatchLogValue) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int64("event_id", value.EventID), slog.String("listener", value.Listener),
		slog.Int("status", value.Status), slog.String("error", value.Error),
	)
}

func newListenerLogValue(listener delivery.ListenerConfig) listenerLogValue {
	method := listener.Method
	if method == "" {
		method = "POST"
	}
	names := make([]string, 0, len(listener.Headers))
	for name := range listener.Headers {
		names = append(names, name)
	}
	slices.Sort(names)
	return listenerLogValue{
		Name: listener.Name, Method: method,
		Destination: redactURL(listener.URL), HeaderNames: names,
	}
}

func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "<redacted>"
	}
	return parsed.Redacted()
}

var logValuerSet = []func() slog.LogValuer{
	func() slog.LogValuer { return config.Config{} },
	func() slog.LogValuer { return listenerLogValue{} },
	func() slog.LogValuer { return dispatchLogValue{} },
}

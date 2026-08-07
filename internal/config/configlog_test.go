package config

import (
	"bytes"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestConfigLogValueRedactsEverySchemaSensitiveClassThroughSlog(t *testing.T) {
	cfg := Config{
		Version:  1,
		Instance: "noty",
		Database: Database{
			URL:       "postgres://noty:DB_PASSWORD@db.internal:5432/noty?sslpassword=SSL_PASSWORD",
			Schema:    "noty",
			ListenURL: "host=replica user=noty password=LISTEN_PASSWORD\rSHARED_DATABASE_TAIL",
		},
		Listeners: []Listener{{
			Name:    "order_paid",
			Enabled: true,
			Delivery: DeliverySpec{Destination: Destination{
				URL:     "https://hook:DESTINATION_PASSWORD@hooks.example.test/order-paid",
				Method:  "POST",
				Headers: Headers{"Z-Last": "z", "A-First": "a"},
				Signing: Signing{Secrets: []string{"SIGNING_ONE", "SIGNING_TWO\rSHARED_SIGNING_TAIL"}},
			}},
		}},
	}

	first := logConfig(t, cfg)
	second := logConfig(t, cfg)
	if first != second {
		t.Errorf("LogValue output is nondeterministic:\nfirst:  %s\nsecond: %s", first, second)
	}
	for _, forbidden := range []string{
		"DB_PASSWORD", "SSL_PASSWORD", "LISTEN_PASSWORD", "DESTINATION_PASSWORD",
		"SIGNING_ONE", "SIGNING_TWO", "SHARED_DATABASE_TAIL", "SHARED_SIGNING_TAIL",
	} {
		if strings.Contains(first, forbidden) {
			t.Errorf("slog output contains forbidden bytes %q:\n%s", forbidden, first)
		}
	}
	for _, useful := range []string{
		"postgres://noty:[redacted]@db.internal:5432/noty?sslpassword=[redacted]",
		"https://hook:[redacted]@hooks.example.test/order-paid",
		// The header *names*, which are what this log is for. Their values are deliberately absent:
		// a destination header is where an operator puts the credential their endpoint expects, so
		// logHeaders records which headers exist and never what they hold.
		`"A-First"`, `"Z-Last"`,
	} {
		if !strings.Contains(first, useful) {
			t.Errorf("slog output omits useful redacted value %q:\n%s", useful, first)
		}
	}
	if strings.Count(first, redactionPlaceholder) < 5 {
		t.Errorf("slog output contains %d redaction placeholders, want at least five:\n%s",
			strings.Count(first, redactionPlaceholder), first)
	}
}

func TestConfigLogValueReadsSensitivityFromTheSchemaTable(t *testing.T) {
	level := schemaLevels[levelDatabase]
	original := level.keys
	level.keys = append([]keySpec(nil), original...)
	for i := range level.keys {
		if level.keys[i].name == "schema" {
			level.keys[i].logSensitivity = entireValue
		}
	}
	schemaLevels[levelDatabase] = level
	t.Cleanup(func() {
		level.keys = original
		schemaLevels[levelDatabase] = level
	})

	output := logConfig(t, Config{Database: Database{Schema: "SCHEMA_FROM_TABLE"}})
	if strings.Contains(output, "SCHEMA_FROM_TABLE") {
		t.Errorf("LogValue ignored schema-declared sensitivity:\n%s", output)
	}
	if !strings.Contains(output, redactionPlaceholder) {
		t.Errorf("LogValue did not use the shared redaction placeholder:\n%s", output)
	}
}

func TestConfigLogValueHidesEachSensitiveExtentIndependently(t *testing.T) {
	tests := []struct {
		name      string
		forbidden string
		config    Config
	}{
		{
			name:      "database URL password",
			forbidden: "DATABASE_PASSWORD",
			config: Config{Database: Database{
				URL: "postgres://noty:DATABASE_PASSWORD@db.internal/noty",
			}},
		},
		{
			name:      "database listen URL password",
			forbidden: "LISTEN_PASSWORD",
			config: Config{Database: Database{
				ListenURL: "host=db user=noty password=LISTEN_PASSWORD",
			}},
		},
		{
			name:      "destination URL password",
			forbidden: "DESTINATION_PASSWORD",
			config: Config{Listeners: []Listener{{Delivery: DeliverySpec{
				Destination: Destination{URL: "https://hook:DESTINATION_PASSWORD@hooks.example/x"},
			}}}},
		},
		{
			name:      "signing secret",
			forbidden: "SIGNING_SECRET",
			config: Config{Listeners: []Listener{{Delivery: DeliverySpec{
				Destination: Destination{Signing: Signing{Secrets: []string{"SIGNING_SECRET"}}},
			}}}},
		},
		{
			name:      "sensitive physical line tail",
			forbidden: "SHARED_PHYSICAL_TAIL",
			config: Config{Database: Database{
				URL: "postgres://noty:password@db/noty\rSHARED_PHYSICAL_TAIL",
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if output := logConfig(t, tc.config); strings.Contains(output, tc.forbidden) {
				t.Errorf("slog output contains %q:\n%s", tc.forbidden, output)
			}
		})
	}
}

func logConfig(t *testing.T, cfg Config) string {
	t.Helper()
	var output bytes.Buffer
	handler := slog.NewJSONHandler(&output, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return attr
		},
	})
	slog.New(handler).Info("loaded", slog.Any("config", cfg))
	return output.String()
}

// TestLoggedHeaderNamesAreEmittedInSortedOrder pins the one map this log surface ranges over.
//
// The whole-corpus determinism gate reaches Config.LogValue, but it reaches it through whichever
// header maps the fixtures happen to declare, and Go's per-range randomisation means a small map
// often iterates in sorted order anyway -- so that gate catches an unsorted emission only some of the
// time. This asserts the order directly, against the sorted names rather than against a list written
// beside them, and does it with enough names that agreeing with sorted order by chance is not a way
// to pass.
func TestLoggedHeaderNamesAreEmittedInSortedOrder(t *testing.T) {
	headers := Headers{
		"X-Zulu": "z", "X-Alpha": "a", "X-Mike": "m", "X-Charlie": "c",
		"X-Papa": "p", "X-Bravo": "b", "X-Tango": "t", "X-Delta": "d",
	}

	emitted := make([]string, 0, len(headers))
	for _, attr := range logHeaders(headers).Value.Group() {
		emitted = append(emitted, attr.Key)
	}

	want := slices.Sorted(maps.Keys(headers))
	if !slices.Equal(emitted, want) {
		t.Errorf("logHeaders emitted %v, want %v; a log line whose field order follows map "+
			"iteration differs between two runs of one binary", emitted, want)
	}
}

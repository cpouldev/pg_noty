//go:build integration

package source

import (
	"encoding/json"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func eventPayload(t *testing.T, pool *pgxpool.Pool) map[string]any {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(
		t.Context(),
		"SELECT payload FROM noty.events ORDER BY id DESC LIMIT 1",
	).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestPayloadModesReadBackPresenceAndAbsence(t *testing.T) {
	skipIfShort(t)
	cases := []struct {
		name    string
		payload config.Payload
		want    []string
		omit    []string
	}{
		{"full", config.Payload{Mode: "full"}, []string{"id", "status", "secret", "other"}, nil},
		{
			"full exclude", config.Payload{Mode: "full", Exclude: []string{"secret"}},
			[]string{"id", "status", "other"}, []string{"secret"},
		},
		{
			"columns", config.Payload{Mode: "columns", Columns: []string{"status"}}, []string{"status"},
			[]string{"id", "secret", "other"},
		},
		{"keys only", config.Payload{Mode: "keys_only"}, []string{"id", "other"}, []string{"status", "secret"}},
	}
	for _, testCase := range cases {
		t.Run(
			testCase.name, func(t *testing.T) {
				pool := freshDatabase(t)
				applySourceMigrations(t, pool)
				mustExecOn(
					t,
					pool,
					`CREATE TABLE public.payload_target (id int, other int, status text, secret text, PRIMARY KEY (id, other))`,
				)
				request := generationRequest(config.Operation{Kind: "insert"})
				request.Target.Table, request.Target.PrimaryKeyColumns = "payload_target", []string{"id", "other"}
				request.Listener.Trigger.Payload = testCase.payload
				sets, err := Generate(request)
				if err != nil {
					t.Fatal(err)
				}
				executeObjectSet(t, pool, sets[0])
				mustExecOn(t, pool, `INSERT INTO public.payload_target VALUES (1, 2, 'visible', 'secret')`)
				newPayload, ok := eventPayload(t, pool)["new"].(map[string]any)
				if !ok {
					t.Fatalf("new payload has wrong shape: %#v", eventPayload(t, pool))
				}
				for _, column := range testCase.want {
					if _, ok := newPayload[column]; !ok {
						t.Errorf("wanted column %q absent from %#v", column, newPayload)
					}
				}
				for _, column := range testCase.omit {
					if _, ok := newPayload[column]; ok {
						t.Errorf("omitted column %q present in %#v", column, newPayload)
					}
				}
			},
		)
	}
}

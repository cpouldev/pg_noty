package config

import (
	"path/filepath"
	"slices"
	"testing"
)

var structuredRejectingFixtures = map[string]semanticFixture{
	"R3_database_url_bad_form":               {R3, "database.url"},
	"R5_database_listen_url_bad_form":        {R5, "database.listen_url"},
	"R19_default_header_name_invalid":        {R19, "defaults.headers.Bad Header"},
	"R19_destination_header_value_empty":     {R19, "listeners[0].destination.headers.X-Trace"},
	"R21_destination_header_reserved":        {R21, "listeners[0].destination.headers.X-Pg-Noty-Event-Id"},
	"R24_listener_name_duplicate":            {R24, "listeners[1].name"},
	"R26_listener_table_unqualified":         {R26, "listeners[0].table"},
	"R26_listener_table_bad_schema_part":     {R26, "listeners[0].table"},
	"R26_listener_table_part_64_bytes":       {R26, "listeners[0].table"},
	"R29_update_columns_duplicate":           {R29, "listeners[0].operations.update.columns[1]"},
	"R29_update_columns_empty_entry":         {R29, "listeners[0].operations.update.columns[0]"},
	"R29_update_columns_invalid_identifier":  {R29, "listeners[0].operations.update.columns[0]"},
	"R30_insert_when_references_old":         {R30, "listeners[0].operations.insert.when"},
	"R30_insert_when_references_lower_old":   {R30, "listeners[0].operations.insert.when"},
	"R30_delete_when_references_new":         {R30, "listeners[0].operations.delete.when"},
	"R32_payload_columns_missing":            {R32, "listeners[0].payload.mode"},
	"R32_payload_columns_empty_list":         {R32, "listeners[0].payload.columns"},
	"R32_payload_columns_with_full":          {R32, "listeners[0].payload.columns"},
	"R32_payload_columns_duplicate":          {R32, "listeners[0].payload.columns[1]"},
	"R32_payload_columns_empty_entry":        {R32, "listeners[0].payload.columns[0]"},
	"R32_payload_columns_invalid_identifier": {R32, "listeners[0].payload.columns[0]"},
	"R33_payload_columns_and_exclude":        {R33, "listeners[0].payload.exclude"},
	"R33_payload_exclude_with_keys_only":     {R33, "listeners[0].payload.exclude"},
	"R33_payload_exclude_duplicate":          {R33, "listeners[0].payload.exclude[1]"},
	"R33_payload_exclude_empty_entry":        {R33, "listeners[0].payload.exclude[0]"},
	"R33_payload_exclude_invalid_identifier": {R33, "listeners[0].payload.exclude[0]"},
	"R36_destination_url_ftp":                {R36, "listeners[0].destination.url"},
	"R36_destination_url_empty":              {R36, "listeners[0].destination.url"},
	"R36_destination_url_unparseable":        {R36, "listeners[0].destination.url"},
	"R36_destination_url_without_host":       {R36, "listeners[0].destination.url"},
	"R38_signing_secret_empty_entry":         {R38, "listeners[0].destination.signing.secrets[0]"},
	"R38_signing_secret_duplicate":           {R38, "listeners[0].destination.signing.secrets[1]"},
}

var structuredAcceptingFixtures = []string{
	"R3_ok_connection_uri",
	"R5_ok_connection_keyword_value",
	"R19_ok_header_names_and_values",
	"R21_ok_non_reserved_header",
	"R24_ok_unique_listener_names",
	"R26_ok_table_part_63_bytes",
	"R29_ok_update_column_content",
	"R30_ok_adversarial_when_clauses",
	"R32_ok_payload_columns",
	"R33_ok_payload_exclude",
	"R36_ok_http_and_https_urls",
	"R38_ok_signing_secret_boundaries",
}

func TestEveryStructuredRejectingFixtureRaisesExactlyItsClaim(t *testing.T) {
	for name, want := range structuredRejectingFixtures {
		t.Run(name, func(t *testing.T) {
			path := fixture(name)
			_, _, diags := Parse(readFixtureBytes(t, path), filepath.Base(path), corpusEnvironment())
			if len(diags) != 1 {
				t.Fatalf("got %d diagnostics %q, want one", len(diags), messagesOf(diags))
			}
			got := diags[0]
			if got.Rule != want.rule || got.Path != want.path || got.Line == 0 || got.Col == 0 ||
				got.Msg == "" {
				t.Errorf("diagnostic = %+v, want actionable %s at %s", got, want.rule, want.path)
			}
		})
	}
}

func TestEveryStructuredAcceptingFixtureIsClean(t *testing.T) {
	for _, name := range structuredAcceptingFixtures {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(validCorpus, name+fixtureExtension)
			_, _, diags := Parse(readFixtureBytes(t, path), filepath.Base(path), corpusEnvironment())
			if len(diags) != 0 {
				t.Fatalf("valid fixture produced %q", messagesOf(diags))
			}
		})
	}
}

func TestFiveMistakeFixtureHasFiveOrderedIndependentDiagnostics(t *testing.T) {
	path := fixture("R41_five_independent_mistakes")
	_, _, diags := Parse(readFixtureBytes(t, path), filepath.Base(path), corpusEnvironment())
	wantRules := []RuleID{R41, R14, R15, R33, R36}
	wantPaths := []string{"database.schmea", "defaults.retry.max_attempts",
		"defaults.retry.backoff", "listeners[0].payload.exclude",
		"listeners[0].destination.url"}
	if len(diags) != len(wantRules) {
		t.Fatalf("got %d diagnostics %q, want five", len(diags), messagesOf(diags))
	}
	for i := range wantRules {
		if diags[i].Rule != wantRules[i] || diags[i].Path != wantPaths[i] {
			t.Errorf("diagnostic %d = %+v, want %s at %s", i, diags[i], wantRules[i], wantPaths[i])
		}
	}
}

func TestStructuredFixtureInventoryCoversItsTwelveOwnedRules(t *testing.T) {
	if len(structuredRejectingFixtures) != 32 || len(structuredAcceptingFixtures) != 12 {
		t.Fatalf("structured fixture inventories = %d rejecting, %d accepting; want 32 and 12",
			len(structuredRejectingFixtures), len(structuredAcceptingFixtures))
	}
	got := make([]RuleID, 0, len(structuredRejectingFixtures))
	for _, fixture := range structuredRejectingFixtures {
		got = append(got, fixture.rule)
	}
	slices.Sort(got)
	got = slices.Compact(got)
	want := []RuleID{R3, R5, R19, R21, R24, R26, R29, R30, R32, R33, R36, R38}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("rejecting rules = %v, want %v", got, want)
	}
}

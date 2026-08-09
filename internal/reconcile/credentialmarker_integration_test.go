//go:build integration

package reconcile

import (
	"bytes"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is the oracle secrets_integration_test.go detects a credential leak with, and nothing
// else. It is the internal/config/leakmarker_test.go twin: that package keeps its marking apart
// from the assertions that use it, and the same split keeps both files inside the 200-line bound.
//
// A search for a planted marker can only see the part of the value that marker covers. A sentinel
// written once at the front of a secret therefore watches its first line and nothing else, so a leak
// of any later line is indistinguishable from a pass. Every line of every planted value is marked at
// both ends, and the assertions search each of those markers individually. Neither internal/config's
// helper nor internal/schema's is reachable here -- both are unexported test declarations elsewhere
// -- so this is a rebuild, not a copy of a callable helper.

const credentialSentinel = "PGNOTY-RECONCILE-CRED-"

// markedCredential is a credential-bearing value whose every line is watched. text is what gets
// planted into the configuration; finding any entry of markers on any surface is a leak.
type markedCredential struct {
	text    string
	markers []string
}

// credentialMarkedOnEveryLine brackets each line of body with its own pair of markers. Both ends are
// marked because a partial render truncates in both directions: a message quoting a value's head and
// one quoting its tail are different leaks, and marking one end only leaves the other invisible. A
// separator follows the line number so that no marker is a substring of another and a report cannot
// name line 1 for a leak of line 11.
func credentialMarkedOnEveryLine(label, body string) markedCredential {
	var text strings.Builder
	var markers []string

	for number, line := range strings.Split(body, "\n") {
		head := credentialSentinel + label + "-" + strconv.Itoa(number+1) + "-HEAD"
		foot := credentialSentinel + label + "-" + strconv.Itoa(number+1) + "-TAIL"
		if number != 0 {
			text.WriteString("\n")
		}
		text.WriteString(head + line + foot)
		markers = append(markers, head, foot)
	}
	return markedCredential{text: text.String(), markers: markers}
}

// credentialPlanter accumulates the markers of every value it marks, so the oracle's watch list is
// built by the act of planting rather than transcribed beside it -- a transcription is what goes
// stale when a field is added.
type credentialPlanter struct{ markers []string }

func (planter *credentialPlanter) mark(label, body string) string {
	marked := credentialMarkedOnEveryLine(label, body)
	planter.markers = append(planter.markers, marked.markers...)
	return marked.text
}

// plantedCredentials is a configuration carrying a marked value in every field this package is handed
// that can hold a credential: the connection string, the webhook URL, an authorization header and
// two signing secrets, the first of which spans three lines. Instance and Schema are deliberately
// left unmarked -- neither is a credential, and Instance is written into every ownership marker by
// design, so marking it would plant a leak this package is required to produce.
type plantedCredentials struct {
	cfg      config.Config
	listener config.Listener
	markers  []string
}

func plantCredentials(t *testing.T, table string) plantedCredentials {
	t.Helper()

	var planter credentialPlanter
	listener := matrixListener(table, "insert")
	listener.Delivery.Destination = config.Destination{
		URL:     planter.mark("webhook", "https://example.invalid/hook?token=abc"),
		Method:  "POST",
		Headers: config.Headers{"Authorization": planter.mark("authtoken", "Bearer abc")},
		Signing: config.Signing{
			Secrets: []string{
				planter.mark("signing1", "first-secret\nsecond line of it\nthird line of it"),
				planter.mark("signing2", "rotated-secret"),
			},
		},
	}
	cfg := harnessConfig(t)
	cfg.Database.URL = planter.mark("dsn", "postgres://noty:pw@127.0.0.1:5432/db")
	cfg.Listeners = []config.Listener{listener}
	return plantedCredentials{cfg: cfg, listener: listener, markers: planter.markers}
}

// runOutcome is one run's whole rendered output. err and logged are kept whatever the run did, so
// every surface is searched on the three failing runs as well as on the two clean ones.
type runOutcome struct {
	plan   PlanResult
	logged string
	err    error
}

// surfaces is every rendered form a run can produce, keyed by what it is, so a failure says which
// surface leaked rather than only that something did.
func (outcome runOutcome) surfaces() map[string]string {
	rendered := map[string]string{
		"plan output": outcome.plan.Render(),
		"log lines":   outcome.logged,
		"error text":  "",
	}
	if outcome.err != nil {
		rendered["error text"] = outcome.err.Error()
	}
	var refusals, diagnostics strings.Builder
	for _, refusal := range outcome.plan.Refusals {
		refusals.WriteString(refusal.Message() + "\n" + refusal.Remediation() + "\n")
	}
	for _, diagnostic := range outcome.plan.Diagnostics {
		diagnostics.WriteString(
			strings.Join(
				[]string{
					diagnostic.Msg, diagnostic.Hint,
					diagnostic.Path, diagnostic.File, string(diagnostic.Rule),
				}, "\n",
			),
		)
	}
	rendered["refusals"], rendered["diagnostics"] = refusals.String(), diagnostics.String()
	return rendered
}

// markersReaching is every leak: one entry per surface that quotes a marker, naming both. It is a
// mechanism returning a value rather than a guard that reports, so the control below can drive the
// search the assertions really run instead of a second expression of it. Every marker is searched
// for individually; searching only the sentinel that opens them would find the first leak and say
// nothing about which line leaked.
func markersReaching(surfaces map[string]string, markers []string) []string {
	var leaks []string
	for _, surface := range slices.Sorted(maps.Keys(surfaces)) {
		for _, marker := range markers {
			if strings.Contains(surfaces[surface], marker) {
				leaks = append(leaks, surface+" quotes "+marker)
			}
		}
	}
	return leaks
}

func assertNoMarkerReaches(t *testing.T, run string, surfaces map[string]string, markers []string) {
	t.Helper()
	if len(markers) == 0 {
		t.Fatalf("%s: nothing was planted, so this oracle would see nothing", run)
	}
	for _, leak := range markersReaching(surfaces, markers) {
		t.Errorf("%s: %s", run, leak)
	}
}

// TestTheCredentialOracleSeesAMarkerOnEveryLineOfAPlantedValue is what makes every assertion built
// on this oracle falsifiable: without it a search that found nothing is indistinguishable from one
// that could find nothing, and the whole credential suite passes by being blind. Each line's markers
// are planted alone and exactly one leak is required, so this also pins the separator keeping line
// 1's marker out of line 11's. The clean surface is the guard's other side.
func TestTheCredentialOracleSeesAMarkerOnEveryLineOfAPlantedValue(t *testing.T) {
	skipIfShort(t)

	marked := credentialMarkedOnEveryLine("control", "a planted value\nspanning two lines\nand a third")
	if want := 3 * 2; len(marked.markers) != want {
		t.Fatalf(
			"a three-line value carries %d markers, want %d: one pair per line",
			len(marked.markers), want,
		)
	}
	for _, marker := range marked.markers {
		leaks := markersReaching(map[string]string{"probe": "before " + marker + " after"}, marked.markers)

		if len(leaks) != 1 || !strings.Contains(leaks[0], marker) {
			t.Errorf("a surface quoting %s produced %v, want exactly one leak naming it", marker, leaks)
		}
	}
	if leaks := markersReaching(map[string]string{"probe": "nothing planted here"}, marked.markers); len(leaks) != 0 {
		t.Errorf("a surface quoting no marker produced %v, so the oracle over-fires", leaks)
	}
}

// recordedRun drives one Plan or Apply with a debug logger attached, so the log surface is captured
// at the level that renders the most.
func recordedRun(t *testing.T, pool *pgxpool.Pool, cfg config.Config, opts Options, apply bool) runOutcome {
	t.Helper()

	var logged bytes.Buffer
	opts.Logger = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if !apply {
		plan, err := Plan(t.Context(), pool, cfg, opts)
		return runOutcome{plan: plan, logged: logged.String(), err: err}
	}
	result, err := Apply(t.Context(), pool, cfg, Approval{Approved: true, DestructionPermitted: true}, opts)
	return runOutcome{plan: result.Plan, logged: logged.String(), err: err}
}

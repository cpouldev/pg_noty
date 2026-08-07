package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

type corpusDeterminismSnapshot struct {
	rendered string
	ordering string
	// logged is what Config.LogValue renders to. It is a public output surface of its own, and the
	// gate used to stop at the diagnostics: the configuration was discarded at the Parse call, so
	// every map this package ranges over on the way to a log line was outside the claim.
	logged string

	// configurations is how many fixtures actually yielded one, which is the population the
	// emptiness check below has to be written over. loggedFormOf substitutes a stand-in for a
	// fixture that yielded none, and that stand-in is non-empty *on purpose* -- so a corpus which
	// stopped producing configurations altogether still folded into a non-empty `logged`, and
	// neutralising loggedFormOf's real branch left the whole suite green.
	configurations int
}

func TestWholeRecursiveCorpusRenderingIsDeterministic(t *testing.T) {
	first := observeRecursiveCorpus(t)
	second := observeRecursiveCorpus(t)

	assertCorpusSnapshotEqual(t, "second run in one process", first, second)
	if first.rendered == "" || first.ordering == "" || first.logged == "" {
		t.Fatal("whole-corpus determinism snapshot is empty")
	}
	if first.configurations == 0 {
		t.Fatal("no fixture yielded a configuration, so the logged dimension covers nothing but " +
			"stand-ins and Config.LogValue is outside the determinism claim again")
	}
	if first.configurations != second.configurations {
		t.Errorf("the two runs yielded %d and %d configurations; the population itself is not stable",
			first.configurations, second.configurations)
	}
}

func TestDeterminismGateRejectsASyntheticOrderPerturbation(t *testing.T) {
	ordered := Errors{
		{File: "a.yaml", Line: 1, Col: 1, Path: "alpha", Msg: "must be first"},
		{File: "a.yaml", Line: 2, Col: 1, Path: "beta", Msg: "must be second"},
	}
	if issues := diagnosticOrderIssues(ordered); len(issues) != 0 {
		t.Fatalf("ordered control failed: %v", issues)
	}
	ordered[0], ordered[1] = ordered[1], ordered[0]
	if issues := diagnosticOrderIssues(ordered); len(issues) == 0 {
		t.Fatal("synthetically perturbed diagnostic order passed the determinism gate")
	}

	baseline := corpusDeterminismSnapshot{rendered: "first\nsecond\n", ordering: "1|2\n", logged: "a"}
	perturbed := corpusDeterminismSnapshot{rendered: "second\nfirst\n", ordering: "2|1\n", logged: "a"}
	if corpusSnapshotsEqual(baseline, perturbed) {
		t.Fatal("synthetically perturbed corpus bytes passed the determinism gate")
	}

	// The logged dimension is perturbed on its own, so a gate that had stopped comparing it -- the
	// dimension the vacuity count above guards the population of -- fails here rather than passing
	// on the rendered bytes alone.
	relogged := baseline
	relogged.logged = "b"
	if corpusSnapshotsEqual(baseline, relogged) {
		t.Fatal("a snapshot differing only in its logged form passed the determinism gate")
	}
}

func observeRecursiveCorpus(t *testing.T) corpusDeterminismSnapshot {
	t.Helper()
	paths := recursiveYAMLFixtures(t, invalidCorpus, validCorpus, filepath.Join("testdata", "warnings"))
	var rendered, ordering, logged strings.Builder
	configurations := 0
	for _, path := range paths {
		data := readFixtureBytes(t, path)
		display := filepath.ToSlash(path)
		cfg, warnings, errs := Parse(data, display, corpusEnvironmentFor(path))
		assertDiagnosticOrder(t, display+" errors", errs)
		assertDiagnosticOrder(t, display+" warnings", errorsFromWarnings(warnings))

		fmt.Fprintf(&rendered, "== %s ==\n%s%s", display, errs.Render(data), warnings.Render(data))
		fmt.Fprintf(&ordering, "== %s errors ==\n%s", display, fingerprint(errs))
		fmt.Fprintf(&ordering, "== %s warnings ==\n%s", display,
			fingerprint(errorsFromWarnings(warnings)))
		if cfg != nil {
			configurations++
		}
		fmt.Fprintf(&logged, "== %s ==\n%s\n", display, loggedFormOf(cfg))
	}
	return corpusDeterminismSnapshot{
		rendered:       rendered.String(),
		ordering:       ordering.String(),
		logged:         logged.String(),
		configurations: configurations,
	}
}

// loggedFormOf is what a handler would write for a configuration, or a fixed stand-in for a fixture
// that produced none. The stand-in is a value rather than an empty string so that a reader can see
// the degeneration in the snapshot -- which is exactly why the snapshot's own emptiness cannot be
// the guard against it, and why the count of real configurations is carried beside the bytes.
func loggedFormOf(cfg *Config) string {
	if cfg == nil {
		return "(no configuration)"
	}
	return cfg.LogValue().String()
}

// corpusEnvironmentFor is the environment one fixture is parsed with: the shared corpus variables,
// overridden by whatever scenario that fixture declares.
func corpusEnvironmentFor(path string) EnvLookup {
	values := make(map[string]string, len(corpusVariables)+4)
	for name, value := range corpusVariables {
		values[name] = value
	}
	if scenario, exists := coverageEnvironmentScenarios[filepath.Base(path)]; exists {
		for name, value := range scenario.values {
			values[name] = value
		}
	}
	return MapEnv(values)
}

func assertDiagnosticOrder(t *testing.T, name string, diags Errors) {
	t.Helper()
	if issues := diagnosticOrderIssues(diags); len(issues) != 0 {
		t.Errorf("%s is not strictly ordered by (File, Line, Col, Path, Msg): %v", name, issues)
	}
}

// diagnosticOrderIssues reports every adjacent pair that is not *strictly* increasing, which is the
// claim compareByPosition's own comment makes: the key is total over a de-duplicated collection, and
// a collection is all this ever sees. A monotonic check would accept a tie, and a tie is exactly the
// state in which the order falls back on whichever rule happened to run first.
func diagnosticOrderIssues(diags Errors) []string {
	var issues []string
	for i := 1; i < len(diags); i++ {
		if compareDiagnosticTuple(diags[i-1], diags[i]) >= 0 {
			issues = append(issues, fmt.Sprintf("%d does not strictly precede %d", i-1, i))
		}
	}
	return issues
}

func assertCorpusSnapshotEqual(t *testing.T, name string, want, got corpusDeterminismSnapshot) {
	t.Helper()
	if !corpusSnapshotsEqual(want, got) {
		t.Errorf("%s changed whole-corpus bytes", name)
	}
}

func corpusSnapshotsEqual(a, b corpusDeterminismSnapshot) bool {
	return a.rendered == b.rendered && a.ordering == b.ordering && a.logged == b.logged
}

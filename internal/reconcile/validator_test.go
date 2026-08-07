package reconcile

import (
	"context"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func TestValidatorRejectsAnUnknownDeferredKindThroughError(t *testing.T) {
	validator := &Validator{}
	diagnostics, err := validator.Validate(context.Background(), []config.DeferredCheck{{Kind: "future_check"}})
	if err == nil {
		t.Fatal("Validate() accepted an unknown deferred kind")
	}
	if len(diagnostics) != 0 {
		t.Fatalf("Validate() diagnostics = %#v, want none for a package contract violation", diagnostics)
	}
	want := unknownDeferredKindRefusal("future_check").Message()
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Validate() error = %q, want it to name %q", err, want)
	}
}

func TestDeferredCheckPositionPreservesTheConfigToken(t *testing.T) {
	check := config.DeferredCheck{
		File: "config.yaml", Line: 12, Col: 18, Path: "listeners[0].operations.update.columns",
	}
	at := positionedCheck{check: check}
	if got, want := at.File(), check.File; got != want {
		t.Errorf("File() = %q, want %q", got, want)
	}
	if got, want := at.Line(), check.Line; got != want {
		t.Errorf("Line() = %d, want %d", got, want)
	}
	if got, want := at.Col(), check.Col; got != want {
		t.Errorf("Col() = %d, want %d", got, want)
	}
	if got, want := at.Path(), check.Path; got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestCheckWhenIsTheOnlyRawSQLInterpolationSite(t *testing.T) {
	files := productionSourceNames(t)
	var sites []string
	hits := 0
	for _, name := range files {
		if !strings.HasPrefix(name, "check") {
			continue
		}
		found := strings.Count(string(sourceBytes(t, name)), "fmt.Sprintf(")
		if found != 0 {
			sites = append(sites, name)
			hits += found
		}
	}
	t.Logf("raw-SQL scan examined %d production files; %d interpolation site in %v", len(files), hits, sites)
	if len(sites) != 1 || sites[0] != "checkwhen.go" || hits != 1 {
		t.Fatalf("raw-SQL interpolation sites = %d in %v, want one in checkwhen.go", hits, sites)
	}
}

// DeferredCheck has File, Line, Col and Path fields, so adding same-named methods directly to it
// does not compile. The named positionedCheck wrapper is the frozen-port-compatible adapter.
var _ config.Positioned = positionedCheck{}

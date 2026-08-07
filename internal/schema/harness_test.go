package schema

import (
	goast "go/ast"
	"go/build/constraint"
	"maps"
	"slices"
	"strings"
	"testing"
)

// This file declares the container harness's shape as values and holds the readers its two scan
// families share. It carries **no build tag** on purpose: everything here is a constant or a source
// scan, so the container-free tier can assert what the tagged tier is built from -- which is the
// only place those assertions are worth anything, since a scan asserting that every
// container-touching file is compile-excluded cannot itself be compile-excluded.

const (
	// harnessImage is pinned to a patch release rather than to the floating 17 tag, because every
	// measurement in this task file was taken on PostgreSQL 17.10 and a tag that moved to 18 would
	// change what those pins mean without changing a line of Go.
	harnessImage = "postgres:17.10-alpine"
	// harnessDatabase is deliberately not the postgres system database (SC-4). Restore drops and
	// recreates its target, which cannot be done to the database the runner is connected through --
	// and modules/postgres defaults POSTGRES_DB to the user name, so a harness that does not say
	// otherwise gets `postgres` and every Restore fails with what reads as a harness bug.
	harnessDatabase = "notyharness"
	harnessUser     = "noty"
	harnessPassword = "noty-harness-secret"
	// harnessSchema is where the eight consuming steps create their objects.
	harnessSchema = "noty"
	// harnessSQLDriver is the database/sql driver name modules/postgres opens its snapshot and
	// restore connections with. It is `pgx` because pgx/v5/stdlib registers that name; without the
	// blank import the open fails and the module falls back to `docker exec psql`.
	harnessSQLDriver = "pgx"
)

// integrationBuildTag compile-excludes the whole container tier. The two paths below are what make
// a source a container one whatever its tag: the container runtime, and the driver registration
// that is only ever imported for the snapshot path.
const (
	integrationBuildTag  = "integration"
	testcontainersModule = "github.com/testcontainers"
	nativeDriverPackage  = "github.com/jackc/pgx/v5/stdlib"
)

var containerModules = []string{testcontainersModule, nativeDriverPackage}

// taggedSources is every Go source of this package that the integration tag gates, which is the
// tier the scans range over. containerModuleSources is the subset that imports a container module,
// which is the set that *must* be gated -- keeping the two apart is what lets
// TestEveryContainerModuleSourceIsCompileExcludedWithoutTheTag be an assertion rather than a
// tautology over its own subject.
func taggedSources(t *testing.T) map[string]*goast.File {
	t.Helper()

	return sourcesWhere(t, func(name string, _ *goast.File) bool {
		declared := buildConstraintOf(t, sourceBytes(t, name))
		return declared != nil && gatesOnTheIntegrationTag(declared)
	})
}

func containerModuleSources(t *testing.T) map[string]*goast.File {
	t.Helper()

	return sourcesWhere(t, func(_ string, file *goast.File) bool {
		return slices.ContainsFunc(importedPaths(file), func(imported string) bool {
			return slices.ContainsFunc(containerModules,
				func(module string) bool { return withinModule(imported, module) })
		})
	})
}

// sourcesWhere is every source of this package a predicate accepts, with the syntax of each. One
// reader for both sets, so neither can come to disagree with the other about what it read
// (.claude/rules/reuse-the-helper-before-copying-it.md).
func sourcesWhere(t *testing.T, accepts func(string, *goast.File) bool) map[string]*goast.File {
	t.Helper()

	accepted := map[string]*goast.File{}
	for _, name := range packageSourceNames(t) {
		file := parsedSource(t, name, sourceBytes(t, name))
		if accepts(name, file) {
			accepted[name] = file
		}
	}
	return accepted
}

// buildConstraintOf is the //go:build expression one source declares, or nil when it declares none.
// The header ends at the package clause, which is where the toolchain stops looking too.
func buildConstraintOf(t *testing.T, data []byte) constraint.Expr {
	t.Helper()

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "package ") {
			return nil
		}
		if !constraint.IsGoBuild(line) {
			continue
		}
		parsed, err := constraint.Parse(line)
		if err != nil {
			t.Fatalf("parse build constraint %q: %v", line, err)
		}
		return parsed
	}
	return nil
}

// gatesOnTheIntegrationTag reports whether one constraint admits its file with the integration tag
// and excludes it without one. Both directions, because a constraint that excluded the file always
// would satisfy a one-sided check while making the tagged tier unrunnable
// (.claude/rules/test-both-sides-of-an-exclusion-guard.md).
func gatesOnTheIntegrationTag(declared constraint.Expr) bool {
	return declared.Eval(func(tag string) bool { return tag == integrationBuildTag }) &&
		!declared.Eval(func(string) bool { return false })
}

// TestTheHarnessScansReadTheFilesTheyClaimTo is the vacuity guard every scan below shares. A reader
// that found no tagged source reports them all correctly guarded, free of socket literals and free
// of fixed ports -- the same answer it gives when they are
// (.claude/rules/count-the-population-a-vacuity-guard-guards.md). The population is counted by
// naming the files the package certainly has.
func TestTheHarnessScansReadTheFilesTheyClaimTo(t *testing.T) {
	tagged := taggedSources(t)
	found := slices.Sorted(maps.Keys(tagged))

	for _, want := range []string{
		"testmain_integration_test.go",
		"pool_integration_test.go",
		"harness_integration_test.go",
	} {
		if _, gated := tagged[want]; !gated {
			t.Errorf("the reader found %d tagged sources %v and %s was not among them, so every "+
				"harness scan is passing over nothing", len(found), found, want)
		}
	}
}

// TestTheHarnessTargetIsNotThePostgresSystemDatabase is SC-4 read off the declaration, and
// TestTheHarnessImageIsPinnedToAPatchRelease is the image pin. Both have a twin against the running
// server -- TestTheHarnessRunsAgainstItsOwnDatabaseRatherThanTheSystemOne and
// TestTheContainerServesThePostgresVersionEveryMeasurementWasTakenOn -- because a constant naming
// the right thing says nothing about what the container serves.
func TestTheHarnessTargetIsNotThePostgresSystemDatabase(t *testing.T) {
	if harnessDatabase == "postgres" {
		t.Error("the harness snapshots the postgres system database, which Restore refuses because " +
			"it cannot drop the database it is connected through")
	}
	if harnessDatabase == "" {
		t.Error("the harness names no database, so modules/postgres defaults it to the user name")
	}
}

func TestTheHarnessImageIsPinnedToAPatchRelease(t *testing.T) {
	const want = "postgres:17.10-alpine"
	if harnessImage != want {
		t.Errorf("harnessImage = %q, want %q; this task file's measurements are pinned to "+
			"PostgreSQL 17.10 and a floating tag would move them silently", harnessImage, want)
	}
}

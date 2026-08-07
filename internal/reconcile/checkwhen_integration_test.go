//go:build integration

package reconcile

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	whenTargetName   = "public.step15_when_target"
	sideEffectClause = "public.step15_immutable_witness()"
	// witnessCallCount reads a sequence rather than counting rows; prepareWhenWitness says why. A
	// sequence that has never been called reports last_value 1 with is_called false, so the
	// never-evaluated reading has to be derived rather than read straight out.
	witnessCallCount = "SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM public.step15_witness_calls"
)

// PgConn.Prepare issues PostgreSQL Parse and Describe messages; unlike EXPLAIN it does not plan or
// execute. The immutable lie below is the exact input that would make EXPLAIN unsafe, and
// TestAnImmutableWhenClauseIsEvaluatedAtPlanTimeEvenOverAnEmptyTarget measures that it fires while
// planning rather than leaving the claim as prose.
func TestAPgxPrepareAnalysesReferencesWithoutPlanningOrExecuting(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareWhenWitness(t, pool)
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	statement := whenProbe(whenTargetName, whenTargetName, sideEffectClause)
	if _, err := tx.Conn().PgConn().Prepare(t.Context(), "", statement, nil); err != nil {
		t.Fatalf("Prepare() = %v, want Parse/Describe to accept the expression", err)
	}
	if got := countOn(t, pool, witnessCallCount); got != 0 {
		t.Fatalf("Parse/Describe evaluated the clause %d times", got)
	}
	assertTheProbeIsExecutable(t, pool)
}

func TestAWhenClauseCarryingASideEffectIsAnalysedAndNeverFires(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareWhenWitness(t, pool)
	check := whenCheck(sideEffectClause)
	diagnostics, err := NewValidator(pool, config.Config{}).Validate(t.Context(), []config.DeferredCheck{check})
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("Validate(side effect) = %#v, %v; want acceptance without execution", diagnostics, err)
	}
	if got := countOn(t, pool, witnessCallCount); got != 0 {
		t.Fatalf("validation evaluated the clause %d times", got)
	}
	assertTheProbeIsExecutable(t, pool)
}

func TestWhenValidationAcceptsLegalSQLAndPositionsAColumnFailure(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	mustExecOn(t, pool, "CREATE TABLE public.step15_when_target (id integer PRIMARY KEY, value integer)")
	legal := whenCheck("NEW.value > 0")
	diagnostics, err := NewValidator(pool, config.Config{}).Validate(t.Context(), []config.DeferredCheck{legal})
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("Validate(legal when) = %#v, %v; want acceptance", diagnostics, err)
	}
	missing := whenCheck("NEW.absent > 0")
	diagnostics, err = NewValidator(pool, config.Config{}).Validate(t.Context(), []config.DeferredCheck{missing})
	if err != nil || len(diagnostics) != 1 {
		t.Fatalf("Validate(missing column) = %#v, %v; want one diagnostic", diagnostics, err)
	}
	assertDiagnosticAt(t, diagnostics, deferredRules[config.WhenParses], 17, 15, "column")
}

func TestWhenValidationRefusesAnAppendedStatementThroughAServerDiagnostic(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	mustExecOn(t, pool, "CREATE TABLE public.step15_when_target (id integer PRIMARY KEY, value integer)")
	malicious := whenCheck("NEW.id > 0); INSERT INTO public.step15_witness VALUES (1); --")
	diagnostics, err := NewValidator(pool, config.Config{}).Validate(t.Context(), []config.DeferredCheck{malicious})
	if err != nil || len(diagnostics) != 1 {
		t.Fatalf("Validate(appended statement) = %#v, %v; want one server diagnostic", diagnostics, err)
	}
	if !strings.Contains(strings.ToLower(diagnostics[0].Msg), "cannot insert multiple commands") {
		t.Fatalf("appended-statement diagnostic = %#v, want PostgreSQL extended-protocol refusal", diagnostics[0])
	}
}

func TestWhenDiagnosticDoesNotAbortLaterCatalogValidation(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	mustExecOn(t, pool, "CREATE TABLE public.step15_when_target (id integer PRIMARY KEY, value integer)")
	mustExecOn(t, pool, "CREATE TABLE public.step15_when_later (id integer)")
	later := config.DeferredCheck{
		Kind: config.ColumnsExist, Schema: "public", Table: "step15_when_later", Columns: []string{"absent"},
		File: "when.yaml", Line: 23, Col: 18, Path: "listeners[1].operations.update.columns",
	}

	diagnostics, err := NewValidator(pool, config.Config{}).Validate(
		t.Context(),
		[]config.DeferredCheck{whenCheck("NEW.absent > 0"), later},
	)
	if err != nil || len(diagnostics) != 2 {
		t.Fatalf("Validate(invalid when then column) = %#v, %v; want both diagnostics and nil error", diagnostics, err)
	}
	assertDiagnosticAt(t, diagnostics, deferredRules[config.WhenParses], 17, 15, "column")
	assertDiagnosticAt(t, diagnostics, deferredRules[config.ColumnsExist], 23, 18, "absent")
}

func whenCheck(clause string) config.DeferredCheck {
	return config.DeferredCheck{
		Kind: config.WhenParses, Schema: "public", Table: "step15_when_target", When: clause,
		File: "when.yaml", Line: 17, Col: 15, Path: "listeners[0].operations.insert.when",
	}
}

// prepareWhenWitness builds the side effect a when-clause probe must never produce and makes it
// observable, which takes more than declaring it.
//
// The witness is a sequence rather than a table, and that is the whole of it. validator.go analyses
// each clause inside a savepoint it always rolls back, inside an outer transaction it also always
// rolls back, so a row written by an executed clause is discarded before any pool read can reach it. A
// rolled-back execution then looks exactly like no execution, and the two assertions above pass
// against the fully executing design the probe exists to forbid. A sequence advance is not undone
// by rollback -- and "was it evaluated", not "was it committed", is the question the security NFR
// asks: a clause that ran and was rolled back still ran, having taken whatever locks and read whatever
// files it named.
//
// The target holds a row so the clause is evaluated row-wise and not only while planning. That is not
// what makes an execution observable: planning alone fires this clause over an empty table, which
// TestAnImmutableWhenClauseIsEvaluatedAtPlanTimeEvenOverAnEmptyTarget measures. It keeps the fixture a
// realistic trigger predicate rather than one the server can answer without looking at a row.
func prepareWhenWitness(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	mustExecOn(t, pool, "CREATE TABLE "+whenTargetName+" (id integer PRIMARY KEY, value integer)")
	mustExecOn(t, pool, "INSERT INTO "+whenTargetName+" (id, value) VALUES (1, 1)")
	mustExecOn(t, pool, "CREATE SEQUENCE public.step15_witness_calls")
	mustExecOn(
		t,
		pool,
		`CREATE FUNCTION public.step15_write_witness() RETURNS boolean LANGUAGE plpgsql VOLATILE AS $$ BEGIN PERFORM nextval('public.step15_witness_calls'); RETURN true; END $$`,
	)
	mustExecOn(
		t,
		pool,
		`CREATE FUNCTION public.step15_immutable_witness() RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS $$ BEGIN RETURN public.step15_write_witness(); END $$`,
	)
}

// TestAnImmutableWhenClauseIsEvaluatedAtPlanTimeEvenOverAnEmptyTarget measures the fact the header
// above asserts, and refutes the obvious guess about the two assertions before it.
//
// The guess is that an empty target makes the probe harmless, because the cross product is empty and
// the clause is never evaluated against a row. Measured, it is false: PostgreSQL folds an IMMUTABLE
// function with constant arguments while planning, so this clause fires once before a single row is
// examined. That is precisely what would make EXPLAIN unsafe here, and it is why the only thing
// separating an analysing implementation from an executing one is a witness outliving the rollback
// both of them end in.
func TestAnImmutableWhenClauseIsEvaluatedAtPlanTimeEvenOverAnEmptyTarget(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	prepareWhenWitness(t, pool)
	mustExecOn(t, pool, "DELETE FROM "+whenTargetName)

	mustExecOn(t, pool, whenProbe(whenTargetName, whenTargetName, sideEffectClause))
	planned := countOn(t, pool, witnessCallCount)
	if planned == 0 {
		t.Fatal(
			"executing the probe over an empty target evaluated the clause zero times; planning no " +
				"longer fires it, so re-derive what the two assertions above actually rest on",
		)
	}

	// The same probe with a row to examine advances the witness again, so the reading above is a
	// counter and not a value stuck at its first advance.
	mustExecOn(t, pool, "INSERT INTO "+whenTargetName+" (id, value) VALUES (1, 1)")
	mustExecOn(t, pool, whenProbe(whenTargetName, whenTargetName, sideEffectClause))
	if executed := countOn(t, pool, witnessCallCount); executed <= planned {
		t.Fatalf(
			"the probe over one row left the witness at %d, want more than the %d planning alone "+
				"reached", executed, planned,
		)
	}
}

// assertTheProbeIsExecutable executes the statement its callers only analysed. Without it a zero
// witness reading is satisfied by a clause nothing could ever have evaluated, and the assertion that
// validation never executes holds just as well over a witness that cannot record.
func assertTheProbeIsExecutable(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	mustExecOn(t, pool, whenProbe(whenTargetName, whenTargetName, sideEffectClause))
	if got := countOn(t, pool, witnessCallCount); got == 0 {
		t.Fatal(
			"executing the probe evaluated the clause zero times, so every zero measured against " +
				"this witness is vacuous",
		)
	}
}

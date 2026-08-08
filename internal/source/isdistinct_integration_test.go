//go:build integration

package source

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIsDistinctSelectedColumnsSuppressOnlyUnchangedWatchedValues(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.distinct_target (
		id int PRIMARY KEY,
		"Status" text,
		"a""b" text,
		nullable text,
		document_json json,
		document_jsonb jsonb,
		other int
	)`)
	operation := config.Operation{
		Kind: "update",
		Columns: []string{
			"Status", `a"b`, "nullable", "document_json", "document_jsonb",
		},
		IsDistinct: true,
	}
	set := installDistinctUpdate(t, pool, "distinct_target", operation)
	if filtered := triggerInventoryOf(t, pool, "public", "distinct_target")[set.TriggerName].filtered; len(filtered) != 0 {
		t.Fatalf("is_distinct trigger has UPDATE OF columns %v, want a plain UPDATE trigger", filtered)
	}

	mustExecOn(t, pool, `INSERT INTO public.distinct_target
		(id, "Status", "a""b", nullable, document_json, document_jsonb, other)
		VALUES (1, 'same', 'quoted', NULL, '{"a":1,"b":2}', '{"a":1,"b":2}', 1)`)
	mustExecOn(t, pool, `UPDATE public.distinct_target SET
		"Status"="Status",
		"a""b"="a""b",
		nullable=NULL,
		document_json='{"b":2,"a":1}'::json,
		document_jsonb='{"b":2,"a":1}'::jsonb
		WHERE id=1`)
	assertDistinctEventCount(t, pool, 0, "unchanged nullable, JSON, JSONB, and quoted values")

	mustExecOn(t, pool, `UPDATE public.distinct_target SET other=2 WHERE id=1`)
	assertDistinctEventCount(t, pool, 0, "an unrelated-column change")

	mustExecOn(t, pool, `UPDATE public.distinct_target SET "Status"='changed' WHERE id=1`)
	assertDistinctEventCount(t, pool, 1, "a mixed-case watched-column change")

	mustExecOn(t, pool, `UPDATE public.distinct_target SET nullable='present' WHERE id=1`)
	assertDistinctEventCount(t, pool, 2, "a NULL-to-value watched-column change")

	mustExecOn(t, pool, `UPDATE public.distinct_target SET document_json='{"a":1,"b":3}' WHERE id=1`)
	assertDistinctEventCount(t, pool, 3, "a JSON watched-column change")

	mustExecOn(t, pool, `UPDATE public.distinct_target SET document_jsonb='{"a":1,"b":3}' WHERE id=1`)
	assertDistinctEventCount(t, pool, 4, "a JSONB watched-column change")
}

func TestIsDistinctWholeRowSuppressesNoOpUpdates(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.distinct_row_target (id int PRIMARY KEY, value text, other int)`)
	installDistinctUpdate(t, pool, "distinct_row_target",
		config.Operation{Kind: "update", IsDistinct: true})
	mustExecOn(t, pool, `INSERT INTO public.distinct_row_target VALUES (1, 'same', 1)`)

	mustExecOn(t, pool, `UPDATE public.distinct_row_target SET other=other WHERE id=1`)
	assertDistinctEventCount(t, pool, 0, "a whole-row no-op update")

	mustExecOn(t, pool, `UPDATE public.distinct_row_target SET other=2 WHERE id=1`)
	assertDistinctEventCount(t, pool, 1, "a whole-row value change")
}

func TestIsDistinctSeesWatchedValuesChangedByABeforeUpdateTrigger(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, `CREATE TABLE public.distinct_before_target (
		id int PRIMARY KEY, watched int, other text
	)`)
	mustExecOn(t, pool, `CREATE FUNCTION public.change_watched_before_update() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			IF NEW.other = 'force' THEN
				NEW.watched := OLD.watched + 1;
			END IF;
			RETURN NEW;
		END
		$$`)
	mustExecOn(t, pool, `CREATE TRIGGER change_watched_before_update
		BEFORE UPDATE ON public.distinct_before_target
		FOR EACH ROW EXECUTE FUNCTION public.change_watched_before_update()`)
	set := installDistinctUpdate(t, pool, "distinct_before_target", config.Operation{
		Kind: "update", Columns: []string{"watched"}, IsDistinct: true,
	})
	if filtered := triggerInventoryOf(t, pool, "public", "distinct_before_target")[set.TriggerName].filtered; len(filtered) != 0 {
		t.Fatalf("is_distinct trigger has UPDATE OF columns %v, want none", filtered)
	}
	mustExecOn(t, pool, `INSERT INTO public.distinct_before_target VALUES (1, 10, 'same')`)

	mustExecOn(t, pool, `UPDATE public.distinct_before_target SET other='same' WHERE id=1`)
	assertDistinctEventCount(t, pool, 0, "an unrelated no-op before-trigger path")

	// watched is absent from the original SET list. An UPDATE OF watched trigger would miss this;
	// the plain UPDATE trigger evaluates after the BEFORE trigger has changed NEW.watched.
	mustExecOn(t, pool, `UPDATE public.distinct_before_target SET other='force' WHERE id=1`)
	assertDistinctEventCount(t, pool, 1, "a watched value changed by a BEFORE UPDATE trigger")
}

func installDistinctUpdate(t *testing.T, pool *pgxpool.Pool, table string,
	operation config.Operation,
) ObjectSet {
	t.Helper()
	request := generationRequest(operation)
	request.Target.Table = table
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 1 {
		t.Fatalf("distinct update generated %d object sets, want one", len(sets))
	}
	executeObjectSet(t, pool, sets[0])
	return sets[0]
}

func assertDistinctEventCount(t *testing.T, pool *pgxpool.Pool, want int, scenario string) {
	t.Helper()
	if got := rowCountOf(t, pool, "noty.events"); got != want {
		t.Fatalf("%s produced %d events, want %d", scenario, got, want)
	}
}

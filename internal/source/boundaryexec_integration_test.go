//go:build integration

package source

import (
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
	"github.com/jackc/pgx/v5/pgxpool"
)

const theBoundaryTable = "boundary_target"

// theBoundaryTargetColumns are the three columns every hostile position is exercised against: the
// hostile name itself, plus two ordinary ones so an exclude that removed everything and a payload
// that kept everything are told apart.
var theBoundaryTargetColumns = []string{"id", theBoundaryHostileName, "other"}

// createBoundaryTarget builds the table through schema.Quoted, which is the only way a column named
// `a"b,{\}` can be declared at all.
func createBoundaryTarget(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	definitions := "id int"
	for _, column := range theBoundaryTargetColumns[1:] {
		quoted, fault := schema.Quoted(column)
		if fault != schema.IdentifierOK {
			t.Fatalf("column %q is unusable: %s", column, fault)
		}
		definitions += ", " + quoted + " text"
	}
	mustExecOn(t, pool, "CREATE TABLE public."+theBoundaryTable+" ("+definitions+")")
	mustExecOn(t, pool, "INSERT INTO public."+theBoundaryTable+" (id) VALUES (1)")
}

// updateBoundaryRow writes both text columns, so a trigger filtered to the hostile column fires and
// a payload carrying every column has something in each.
func updateBoundaryRow(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	hostile, _ := schema.Quoted(theBoundaryHostileName)
	mustExecOn(
		t, pool, "UPDATE public."+theBoundaryTable+" SET "+hostile+
			" = 'v1', other = 'o1' WHERE id = 1",
	)
}

// TestEveryHostileBoundaryPositionSurvivesTheServer is the execution half the boundary list was
// missing. Reading the payload back is the point: a name quoted wrongly still produces a statement
// the server accepts, and only the stored row says whether it selected the column the operator
// named. The keys are read with jsonb_exists and ->> rather than by comparing the rendered jsonb
// text, so the comparison is byte-exact against the configured name.
func TestEveryHostileBoundaryPositionSurvivesTheServer(t *testing.T) {
	skipIfShort(t)
	for _, position := range theHostileBoundaryPositions {
		t.Run(
			position.name, func(t *testing.T) {
				pool := freshDatabase(t)
				applySourceMigrations(t, pool)
				createBoundaryTarget(t, pool)
				executeObjectSet(t, pool, hostileBoundarySet(t, position, theBoundaryTable))

				updateBoundaryRow(t, pool)
				assertOneEventCarrying(t, pool, position)
			},
		)
	}
}

func assertOneEventCarrying(t *testing.T, pool *pgxpool.Pool, position hostileBoundaryPosition) {
	t.Helper()
	var listener string
	if err := pool.QueryRow(
		t.Context(),
		"SELECT listener FROM noty.events ORDER BY id DESC LIMIT 1",
	).Scan(&listener); err != nil {
		t.Fatalf("the %s position produced no event row: %v", position.name, err)
	}
	if listener != position.wantListener {
		t.Errorf("the event names listener %q, want %q byte for byte", listener, position.wantListener)
	}
	for _, key := range position.payloadHolds {
		if !payloadHasKey(t, pool, key) {
			t.Errorf(
				"the stored payload has no key %q, which the %s position selects",
				key, position.name,
			)
		}
	}
	for _, key := range position.payloadLacks {
		if payloadHasKey(t, pool, key) {
			t.Errorf(
				"the stored payload carries key %q, which the %s position does not select",
				key, position.name,
			)
		}
	}
}

func payloadHasKey(t *testing.T, pool *pgxpool.Pool, key string) bool {
	t.Helper()
	var present bool
	err := pool.QueryRow(
		t.Context(),
		"SELECT jsonb_exists((SELECT payload->'new' FROM noty.events ORDER BY id DESC LIMIT 1), $1)",
		key,
	).Scan(&present)
	if err != nil {
		t.Fatalf("read the stored payload's keys: %v", err)
	}
	return present
}

// TestTheHostileUpdateOfFilterDoesNotFireOnAnotherColumn is the update-columns position's other
// side. Every position above writes the hostile column, so a trigger whose UPDATE OF list was
// dropped altogether fires there too and reads as correct.
func TestTheHostileUpdateOfFilterDoesNotFireOnAnotherColumn(t *testing.T) {
	skipIfShort(t)
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	createBoundaryTarget(t, pool)
	executeObjectSet(t, pool, hostileBoundarySet(t, positionNamed(t, "update columns"), theBoundaryTable))

	mustExecOn(t, pool, "UPDATE public."+theBoundaryTable+" SET other = 'o2' WHERE id = 1")
	if got := rowCountOf(t, pool, "noty.events"); got != 0 {
		t.Fatalf(
			"updating only `other` produced %d events; the UPDATE OF filter names %q",
			got, theBoundaryHostileName,
		)
	}

	updateBoundaryRow(t, pool)
	if got := rowCountOf(t, pool, "noty.events"); got != 1 {
		t.Fatalf(
			"updating the filtered column produced %d events, want 1; the negative above is "+
				"not evidence that the filter is what suppressed it", got,
		)
	}
}

func positionNamed(t *testing.T, name string) hostileBoundaryPosition {
	t.Helper()
	for _, position := range theHostileBoundaryPositions {
		if position.name == name {
			return position
		}
	}
	t.Fatalf("no boundary position is named %q", name)
	return hostileBoundaryPosition{}
}

package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// theBoundaryHostileName carries the five bytes a generated statement can be broken by: a double
// quote that would close an identifier, a comma that would end a column list, a brace pair from the
// array grammar the payload builder refuses, and a backslash that decides between the plain and E”
// literal forms.
const theBoundaryHostileName = `a"b,{\}`

// Its two rendered forms, written out rather than taken from schema.Quoted and quoteLiteral. An
// expectation derived from the renderer under test is satisfied by whatever that renderer does,
// including dropping the name.
const (
	theHostileQuoted  = `"a""b,{\}"`
	theHostileLiteral = `E'a"b,{\\}'`
)

type hostileExpectation struct{ statement, want string }

// hostileBoundaryPosition is one input position, its rendered expectations, and what the row it
// produces must look like once a server has run the statements. Both tiers read this one list, so
// a position cannot be asserted at the boundary and left unexecuted
// (boundaryexec_integration_test.go).
type hostileBoundaryPosition struct {
	name         string
	edit         func(*Request)
	wants        []hostileExpectation
	wantListener string
	payloadHolds []string
	payloadLacks []string
}

// The R29/R32/R33 ceiling pin closes the configuration route; every value below is constructed
// directly at Generate's boundary, which is the second of the two routes a hostile byte can take
// into this generator.
var theHostileBoundaryPositions = []hostileBoundaryPosition{
	{
		name: "listener",
		edit: func(request *Request) { request.Listener.Name = theBoundaryHostileName },
		wants: []hostileExpectation{
			{"CreateFunction", `"noty"."pg_noty_a""b,{\}_upd"()`},
			{"CreateFunction", theHostileLiteral},
			{"CommentFunction", `E'pg_noty:v1:prod:a"b,{\\}:update'`},
			{"CreateTrigger", `CREATE TRIGGER "pg_noty_a""b,{\}_upd"`},
		},
		wantListener: theBoundaryHostileName,
		payloadHolds: []string{"id", theBoundaryHostileName, "other"},
	},
	{
		name: "update columns",
		edit: func(request *Request) {
			request.Listener.Trigger.Operations[0].Columns = []string{theBoundaryHostileName}
		},
		wants:        []hostileExpectation{{"CreateTrigger", "UPDATE OF " + theHostileQuoted}},
		wantListener: "order_paid",
		payloadHolds: []string{"id", theBoundaryHostileName, "other"},
	},
	{
		name: "payload columns",
		edit: func(request *Request) {
			request.Listener.Trigger.Payload.Mode = "columns"
			request.Listener.Trigger.Payload.Columns = []string{theBoundaryHostileName}
		},
		wants: []hostileExpectation{
			{"CreateFunction", "jsonb_build_object(" + theHostileLiteral + ", NEW." + theHostileQuoted + ")"},
		},
		wantListener: "order_paid",
		payloadHolds: []string{theBoundaryHostileName},
		payloadLacks: []string{"id", "other"},
	},
	{
		name: "exclude",
		edit: func(request *Request) {
			request.Listener.Trigger.Payload.Exclude = []string{theBoundaryHostileName}
		},
		wants:        []hostileExpectation{{"CreateFunction", "to_jsonb(NEW) - " + theHostileLiteral}},
		wantListener: "order_paid",
		payloadHolds: []string{"id", "other"},
		payloadLacks: []string{theBoundaryHostileName},
	},
	{
		name: "primary key",
		edit: func(request *Request) {
			request.Listener.Trigger.Payload.Mode = "keys_only"
			request.Target.PrimaryKeyColumns = []string{theBoundaryHostileName}
		},
		wants: []hostileExpectation{
			{"CreateFunction", "jsonb_build_object(" + theHostileLiteral + ", NEW." + theHostileQuoted + ")"},
		},
		wantListener: "order_paid",
		payloadHolds: []string{theBoundaryHostileName},
		payloadLacks: []string{"id", "other"},
	},
}

// hostileBoundarySet renders one position. It is shared with the execution tier so both run the
// same request.
func hostileBoundarySet(t *testing.T, position hostileBoundaryPosition, table string) ObjectSet {
	t.Helper()
	request := generationRequest(config.Operation{Kind: "update"})
	position.edit(&request)
	request.Target.Table = table
	sets, err := Generate(request)
	if err != nil || len(sets) != 1 {
		t.Fatalf("boundary position %s produced %d object sets: %v", position.name, len(sets), err)
	}
	return sets[0]
}

// TestBoundaryHostileNamesUseTheGeneratorRoute asserts where each position's name lands, not merely
// that generation returned something. A generator that dropped every hostile column, emitted
// NEW.a"b unquoted, or built jsonb_build_object() with no pairs still returns one object set whose
// CreateFunction opens with CREATE OR REPLACE FUNCTION.
func TestBoundaryHostileNamesUseTheGeneratorRoute(t *testing.T) {
	for _, position := range theHostileBoundaryPositions {
		t.Run(
			position.name, func(t *testing.T) {
				statements := statementsByFieldName(hostileBoundarySet(t, position, "orders"))
				for _, want := range position.wants {
					if !strings.Contains(statements[want.statement], want.want) {
						t.Errorf(
							"%s does not carry %q:\n%s",
							want.statement, want.want, statements[want.statement],
						)
					}
				}
			},
		)
	}
}

// TestEveryHostileBoundaryPositionCarriesAnExpectation keeps the list above from admitting a row
// that names a position and asserts nothing about it, which is what the shape this test replaced
// did for all five.
func TestEveryHostileBoundaryPositionCarriesAnExpectation(t *testing.T) {
	if len(theHostileBoundaryPositions) != 5 {
		t.Fatalf(
			"%d boundary positions, want the five Generate accepts a name at",
			len(theHostileBoundaryPositions),
		)
	}
	for _, position := range theHostileBoundaryPositions {
		if len(position.wants) == 0 {
			t.Errorf("the %s position asserts nothing about the text it renders", position.name)
		}
		if len(position.payloadHolds)+len(position.payloadLacks) == 0 {
			t.Errorf("the %s position asserts nothing about the row it produces", position.name)
		}
	}
}

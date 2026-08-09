package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

func generationRequest(operations ...config.Operation) Request {
	return Request{
		Instance:      "prod",
		ServiceSchema: "noty",
		Listener: config.Listener{
			Name: "order_paid", Trigger: config.TriggerSpec{
				Table: "public.orders", Operations: operations,
				Payload: config.Payload{Mode: "full"},
			},
		},
		Target: Target{Schema: "public", Table: "orders", PrimaryKeyColumns: []string{"id", "tenant_id"}},
	}
}

func objectSetValues(set ObjectSet) []string {
	return []string{
		set.Operation, set.FunctionName, set.TriggerName, set.Marker, set.CreateFunction,
		set.RevokeExecute, set.CommentFunction, set.CreateTrigger, set.CommentTrigger,
	}
}

func TestGenerateAssemblesTheCanonicalThreeObjectSets(t *testing.T) {
	request := generationRequest(
		config.Operation{Kind: "delete"}, config.Operation{Kind: "insert"}, config.Operation{Kind: "update"},
	)
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(sets) != 3 {
		t.Fatalf("Generate returned %d object sets, want 3", len(sets))
	}
	for index, want := range []string{"insert", "update", "delete"} {
		if sets[index].Operation != want {
			t.Errorf("object set %d has operation %q, want %q", index, sets[index].Operation, want)
		}
		for field, value := range objectSetValues(sets[index]) {
			if value == "" {
				t.Errorf("object set %d field %d is empty", index, field)
			}
		}
	}
}

// The two input sources that must receive identical treatment. They carry *different* hostile
// values, because one value used for both makes a single hit anywhere in the emitted text satisfy
// the whole claim -- and the implementation this rule exists to catch is exactly the one that
// quotes the source it trusts and renders the other raw.
const (
	hostileFromTheListenerSpecification = `a"b`
	hostileFromTheResolvedCatalog       = `c"d`
)

// theQuotedRouteExpectations bind each route to a position of its own. The doubled forms are
// written out here rather than derived from schema.Quoted: an expectation taken from the quoter is
// satisfied by whatever the quoter does, including nothing.
var theQuotedRouteExpectations = []struct{ field, route, want string }{
	{"CreateFunction", "listener specification", `"noty"."pg_noty_a""b_ins"`},
	{"CreateFunction", "resolved catalog", `'"public"."c""d"'`},
	{"RevokeExecute", "listener specification", `"noty"."pg_noty_a""b_ins"`},
	{"CommentFunction", "listener specification", `"noty"."pg_noty_a""b_ins"`},
	{"CreateTrigger", "listener specification", `CREATE TRIGGER "pg_noty_a""b_ins"`},
	{"CreateTrigger", "resolved catalog", `ON "public"."c""d"`},
	{"CommentTrigger", "listener specification", `COMMENT ON TRIGGER "pg_noty_a""b_ins"`},
	{"CommentTrigger", "resolved catalog", `ON "public"."c""d"`},
}

// TestGenerateUsesTheSameQuotingPathForCatalogAndListenerNames is that rule's boundary half. Each
// expectation names the statement it must appear in, so a generator quoting the catalog-sourced
// table and rendering the listener-derived name raw fails the rows belonging to the route it
// dropped, instead of being carried by the route it kept.
func TestGenerateUsesTheSameQuotingPathForCatalogAndListenerNames(t *testing.T) {
	if hostileFromTheListenerSpecification == hostileFromTheResolvedCatalog {
		t.Fatal("both routes carry the same hostile value, so no row can tell them apart")
	}
	request := generationRequest(config.Operation{Kind: "insert"})
	request.Listener.Name = hostileFromTheListenerSpecification
	request.Target.Table = hostileFromTheResolvedCatalog
	sets, err := Generate(request)
	if err != nil {
		t.Fatal(err)
	}

	statements := statementsByFieldName(sets[0])
	for _, want := range theQuotedRouteExpectations {
		if !strings.Contains(statements[want.field], want.want) {
			t.Errorf(
				"%s does not carry %q, the %s route's value doubled through the identifier "+
					"quoter:\n%s", want.field, want.want, want.route, statements[want.field],
			)
		}
	}
	if got := sets[0].FunctionName; got != `pg_noty_a"b_ins` {
		t.Errorf("FunctionName = %q, want the listener specification's own value joined whole", got)
	}
}

func statementsByFieldName(set ObjectSet) map[string]string {
	return map[string]string{
		"CreateFunction": set.CreateFunction, "RevokeExecute": set.RevokeExecute,
		"CommentFunction": set.CommentFunction, "CreateTrigger": set.CreateTrigger,
		"CommentTrigger": set.CommentTrigger,
	}
}

// TestBothInputRoutesAreRepresentedInTheQuotingExpectations keeps the table above crossed on the axis
// it is named for. A table that lost every row of one route would satisfy every remaining row and
// report identical treatment of one source.
func TestBothInputRoutesAreRepresentedInTheQuotingExpectations(t *testing.T) {
	counted := map[string]int{}
	for _, expectation := range theQuotedRouteExpectations {
		counted[expectation.route]++
	}
	for _, route := range []string{"listener specification", "resolved catalog"} {
		if counted[route] == 0 {
			t.Errorf("no quoting expectation belongs to the %s route", route)
		}
	}
}

func allObjectSetFieldsEmpty(sets []ObjectSet) bool {
	for _, set := range sets {
		for _, value := range objectSetValues(set) {
			if value != "" {
				return false
			}
		}
	}
	return true
}

func TestGenerateRefusesBeforeRenderingAtEveryIdentifierPosition(t *testing.T) {
	base := generationRequest(config.Operation{Kind: "update"})
	cases := []struct {
		name string
		edit func(*Request)
	}{
		{"schema", func(request *Request) { request.Target.Schema = "" }},
		{"table", func(request *Request) { request.Target.Table = "bad\x00table" }},
		{
			"payload columns", func(request *Request) {
				request.Listener.Trigger.Payload.Mode = "columns"
				request.Listener.Trigger.Payload.Columns = []string{"a\x00b"}
			},
		},
		{
			"primary key", func(request *Request) {
				request.Listener.Trigger.Payload.Mode = "keys_only"
				request.Target.PrimaryKeyColumns = []string{""}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(
			testCase.name, func(t *testing.T) {
				request := base
				testCase.edit(&request)
				sets, err := Generate(request)
				if err == nil || !allObjectSetFieldsEmpty(sets) {
					t.Fatalf(
						"Generate(%s) returned sets=%#v, err=%v; refusal must emit nothing",
						testCase.name,
						sets,
						err,
					)
				}
			},
		)
	}
}

// The generation chain's purity -- no pool call, no context parameter -- is asserted by
// TestGenerationPurityHasNoPoolCallOrContextParameter in generationscan_test.go, over the chain
// derived from Generate's own call graph, with its synthetic-violation controls next to it.

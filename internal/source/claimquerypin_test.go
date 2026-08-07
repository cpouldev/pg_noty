package source

import (
	"strconv"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
)

// The cross-package pin, and the one transformation it permits.
//
// claim.go renders every service table through internal/schema's quoting authority, because
// database.schema is the author's to choose and merely defaults to noty. The delivery worker's
// schematic -- and therefore internal/schema/testdata/claim_query.sql -- writes that default bare.
// The single
// difference is asserted by its inverse rather than described: unquoting the issued query
// reproduces the fixture byte for byte, so a drift in either text still fails here.
//
// The default-schema comparison alone cannot see a query that renders one reference through the
// authority and leaves the other spelling noty outright, because unquoting reproduces the fixture
// either way. TestANonDefaultServiceSchemaReachesEveryTableTheClaimQueryNames is what closes that,
// and it is the assertion the hardcoded query this file used to pin fails.

const (
	// theFixtureServiceSchema is the schema the fixture spells bare. R4 names it as the suggestion
	// and defaults.go binds database.schema to it;
	// TestTheDefaultServiceSchemaIsTheOneTheFixtureSpells reconciles this literal against what the
	// config layer actually defaults to, so a change there fails rather than leaving this pin
	// comparing against a schema nothing uses.
	theFixtureServiceSchema = "noty"
	// aNonDefaultServiceSchema is a schema R4 admits that is not the default one.
	aNonDefaultServiceSchema = "app"
	// theClaimQueryFixture is internal/schema's committed copy of the delivery query, header included.
	theClaimQueryFixture = "../schema/testdata/claim_query.sql"
)

// claimQueryReferenceCounts is how often the fixture's query names each service table, counted off
// the text rather than recorded from a run: the queue is the UPDATE's target and the subquery's
// FROM, and the event log is named once, in the trailing comment.
var claimQueryReferenceCounts = map[string]int{schema.TableEventQueue: 2, schema.TableEvents: 1}

// fixtureQueryText is the fixture's SQL: everything after the header comment recording where the
// query came from. The boundary is asserted, so a fixture shaped otherwise fails here rather than
// being compared against its own header.
func fixtureQueryText(t *testing.T) string {
	t.Helper()

	parts := strings.SplitN(string(sourceBytes(t, theClaimQueryFixture)), "\n\n", 2)
	if len(parts) != 2 {
		t.Fatal("the claim fixture lost the blank line separating its header from its query")
	}
	return parts[1]
}

// fixtureQueryFor is the fixture's query as it would be written for one service schema: the bare
// spelling, with the schema it names swapped. The occurrence count is asserted first, so a fixture
// that lost a reference cannot let a partial substitution look complete.
func fixtureQueryFor(t *testing.T, serviceSchema string) string {
	t.Helper()

	written := fixtureQueryText(t)
	for table, want := range claimQueryReferenceCounts {
		if got := strings.Count(written, theFixtureServiceSchema+"."+table); got != want {
			t.Fatalf(
				"the fixture names %s.%s %d times, want %d: the swap below would be partial "+
					"and still look complete", theFixtureServiceSchema, table, got, want,
			)
		}
	}
	return strings.ReplaceAll(written, theFixtureServiceSchema+".", serviceSchema+".")
}

// bareServiceReferences undoes the transformation claim.go applies: every service table rendered
// through the quoting authority becomes the bare schema.table spelling the fixture writes. It is
// the inverse of the rendering rather than a second copy of it, so the comparison cannot pass by
// construction.
//
// The tables it knows about are the fixture's, not claim.go's: a query that named a table the
// fixture does not would keep its quoted spelling here and fail the comparison, rather than being
// unquoted into agreement by a set the renderer and the pin shared.
func bareServiceReferences(t *testing.T, query, serviceSchema string) string {
	t.Helper()

	for table := range claimQueryReferenceCounts {
		qualified, err := qualifiedServiceTable(serviceSchema, table)
		if err != nil {
			t.Fatalf("render %s.%s: %v", serviceSchema, table, err)
		}
		query = strings.ReplaceAll(query, qualified, serviceSchema+"."+table)
	}
	return query
}

// claimQueryDivergence says where the issued query, unquoted, parts from the text it must
// reproduce, or reports that it does not. Every assertion in this file runs through it, and so does
// its falsifiability twin.
func claimQueryDivergence(t *testing.T, serviceSchema, want string) string {
	t.Helper()

	issued, err := claimQueryFor(serviceSchema)
	if err != nil {
		t.Fatalf("claimQueryFor(%s): %v", serviceSchema, err)
	}
	return firstQueryDifference(bareServiceReferences(t, issued, serviceSchema), want)
}

// firstQueryDifference names the line on which two queries first disagree, so a failure points at
// the drift rather than printing two blocks to diff by eye.
func firstQueryDifference(issued, want string) string {
	issuedLines, wantLines := strings.Split(issued, "\n"), strings.Split(want, "\n")
	for number := range max(len(issuedLines), len(wantLines)) {
		at := "line " + strconv.Itoa(number+1)
		switch {
		case number >= len(issuedLines):
			return at + " is missing; the fixture holds " + strconv.Quote(wantLines[number])
		case number >= len(wantLines):
			return at + " is issued as " + strconv.Quote(issuedLines[number]) + "; the fixture ends above it"
		case issuedLines[number] != wantLines[number]:
			return at + " is issued as " + strconv.Quote(issuedLines[number]) +
				" and the fixture holds " + strconv.Quote(wantLines[number])
		}
	}
	return ""
}

func TestTheDefaultServiceSchemaIsTheOneTheFixtureSpells(t *testing.T) {
	minimal := "version: 1\ndatabase:\n  url: postgres://noty@db.internal/noty\nlisteners: []\n"
	cfg, _, errs := config.Parse([]byte(minimal), "claimquerypin_test.yaml", config.MapEnv(nil))
	if cfg == nil || len(errs) != 0 {
		t.Fatalf("the minimal document did not load: %v", errs)
	}
	if cfg.Database.Schema != theFixtureServiceSchema {
		t.Fatalf(
			"config defaults database.schema to %s while the fixture spells %s, so this pin "+
				"compares against a schema nothing uses", cfg.Database.Schema, theFixtureServiceSchema,
		)
	}
	if aNonDefaultServiceSchema == theFixtureServiceSchema {
		t.Fatal("the non-default case names the default schema, so it asserts nothing")
	}
}

func TestClaimQueryMatchesTheCommittedFixture(t *testing.T) {
	if where := claimQueryDivergence(t, theFixtureServiceSchema, fixtureQueryText(t)); where != "" {
		t.Fatalf("the issued claim query drifted from internal/schema's committed fixture: %s", where)
	}
	// The transformation the comparison above undoes has to be a real one. If the issued text were
	// already the fixture's, the service tables would be reaching SQL without the quoting authority
	// Ack, Nack and Dead render them with, and the inverse above would be a no-op that agrees with
	// anything.
	issued, err := claimQueryFor(theFixtureServiceSchema)
	if err != nil {
		t.Fatal(err)
	}
	if issued == fixtureQueryText(t) {
		t.Fatal(
			"the issued query is the fixture's own bare spelling, so no service table went " +
				"through the quoting authority and the comparison above cannot fail",
		)
	}
}

func TestANonDefaultServiceSchemaReachesEveryTableTheClaimQueryNames(t *testing.T) {
	where := claimQueryDivergence(t, aNonDefaultServiceSchema, fixtureQueryFor(t, aNonDefaultServiceSchema))
	if where != "" {
		t.Fatalf(
			"the query issued for service schema %s is not the fixture's query with its schema "+
				"swapped: %s", aNonDefaultServiceSchema, where,
		)
	}
}

func TestTheClaimQueryPinFailsOnDriftInEitherText(t *testing.T) {
	if claimQueryDivergence(t, theFixtureServiceSchema, fixtureQueryText(t)+" drift") == "" {
		t.Fatal("the claim query pin accepted a fixture that had drifted")
	}
	if claimQueryDivergence(t, theFixtureServiceSchema, fixtureQueryFor(t, aNonDefaultServiceSchema)) == "" {
		t.Fatal("the claim query pin accepted a query naming a schema the fixture does not")
	}
}

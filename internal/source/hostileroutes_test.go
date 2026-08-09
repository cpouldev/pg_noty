package source

// hostileRoute is how one hostile-input case gets its value in front of this generator, and why
// that route is the reachable one for the position it exercises.
//
// Criterion 14 names exactly two routes for a byte **phase 1 refuses**: the resolved metadata, and
// direct construction at the generator's boundary. A third value is declared below because
// criterion 13 also names a class phase 1 *accepts* — a column whose real name is `Status` — and
// calling that one "the boundary route" would say a live class is guarded, which is the same
// mistake as the reverse. Reading a fixture's row therefore answers both questions a reviewer has:
// which route the value took, and whether the class behind it is live or closed.
type hostileRoute struct {
	name       string
	why        string
	needsPin   bool
	assertedAt string
}

// theCeilingPin is step 10's R29/R32/R33 routing pin. Every guarded case cites it by name, so the
// citation is checkable in one grep and a rename breaks a search rather than rotting
// (.claude/rules/name-the-test-a-comment-claims-exists.md).
const theCeilingPin = "TestRoutingPinExaminesAllThreeRules"

var (
	// Route one. The catalog supplies the position, so no phase-1 rule stands between the byte and
	// the generator: phase 4 reads the target table and the primary-key columns during OID
	// resolution and rename detection. A live class, asserted end to end against a real server.
	routeResolvedMetadata = hostileRoute{
		name:       "resolved metadata (SD-2)",
		why:        "the catalog supplies this position, so no phase-1 rule stands between the byte and the generator",
		assertedAt: "end to end against a real server",
	}
	// Route two. A position configuration supplies, carrying a byte R29, R32 and R33 refuse through
	// identifierDefect's charset rule. The case exists to assert what the generator *does* with the
	// value, not to claim a live route, which is why it names the pin that fails first if that
	// ceiling ever moves.
	routeGeneratorBoundary = hostileRoute{
		name:       "generator boundary",
		why:        "configuration supplies this position and R29/R32/R33 refuse the byte, so the route is guarded rather than live; " + theCeilingPin + " is what fails first if that ceiling moves",
		needsPin:   true,
		assertedAt: "at the generator's boundary, and executed where the position reaches a server",
	}
	// Not one of criterion 14's two, and deliberately so. `Status` is a name phase 1 accepts —
	// internal/config/ident.go names the class itself — so the value is written straight into the
	// request for determinism while the class behind it stays live, and it is asserted end to end.
	// Declaring it as the boundary route would claim a guarded class where there is none.
	routeConfiguredUppercase = hostileRoute{
		name:       "configuration, live class",
		why:        "phase 1 accepts `Status`, so this configuration position is reachable in production as written and needs no ceiling pin",
		assertedAt: "end to end against a table carrying both spellings",
	}
)

// theHostileFiles are the sources carrying this package's injection proof. The population is
// declared rather than globbed so that adding a hostile suite is a visible edit here.
var theHostileFiles = []string{
	"boundaryexec_integration_test.go",
	"hostileboundary_test.go",
	"hostilecolumns_integration_test.go",
	"hostileuppercase_integration_test.go",
	"injection_integration_test.go",
	"injectioncontrols_integration_test.go",
	"literalroundtrip_integration_test.go",
	"onestatement_integration_test.go",
}

// theHostileCases is the route every hostile-input assertion declares. Criterion 14 is scored on
// it: a reader must be able to tell a live class from a guarded one without reconstructing phase
// 1's rules. Where one assertion spans positions of different routes, the row names the route that
// *governs* — the guarded one, because that is the one owing a citation.
var theHostileCases = map[string]hostileRoute{
	"TestCatalogHostileTableIsQuotedAndInert":                                     routeResolvedMetadata,
	"TestWithoutTheQuotingAuthorityTheHostileTargetIsRefused":                     routeResolvedMetadata,
	"TestWithoutTheQuotingAuthorityAnInjectingTableNameExecutesItsInjection":      routeResolvedMetadata,
	"TestCatalogPrimaryKeyQuoteAndExcludeSubtraction":                             routeResolvedMetadata,
	"TestUppercaseConfiguredColumnSelectsExactColumn":                             routeConfiguredUppercase,
	"TestUppercaseUpdateOfColumnFiltersOnTheExactColumn":                          routeConfiguredUppercase,
	"TestWithoutTheQuotingAuthorityTheUppercaseUpdateFilterSelectsTheWrongColumn": routeConfiguredUppercase,
	"TestUppercaseExcludedColumnIsSubtractedByItsExactKey":                        routeConfiguredUppercase,
	"TestWithoutTheQuotingAuthorityTheUppercaseExcludeSubtractionMissesItsColumn": routeConfiguredUppercase,
	"TestBoundaryExcludeNamesAreAbsentAndOtherColumnsRemain":                      routeGeneratorBoundary,
	"TestBoundaryHostileNamesUseTheGeneratorRoute":                                routeGeneratorBoundary,
	"TestEveryHostileBoundaryPositionSurvivesTheServer":                           routeGeneratorBoundary,
	"TestTheHostileUpdateOfFilterDoesNotFireOnAnotherColumn":                      routeGeneratorBoundary,
	"TestGeneratedLiteralAndMarkerValuesRoundTrip":                                routeGeneratorBoundary,
	"TestTheGeneratedChannelAndMarkerRoundTripThroughTheServer":                   routeGeneratorBoundary,
}

// theHostileMetaAssertions police the case list rather than exercising a byte of their own. They
// are named so the partition over theHostileFiles is closed: an assertion there joins this set or
// theHostileCases, and one joining neither fails by name rather than shipping with no route.
var theHostileMetaAssertions = []string{
	"TestEveryHostileBoundaryPositionCarriesAnExpectation",
	"TestTheOneStatementCounterCountsWhatTheServerRan",
}

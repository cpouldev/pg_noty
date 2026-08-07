package schema

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// runnerledger_test.go asserts which reason a ledger is refused for. This file asserts what each
// reason then tells an operator, and that no reason leaves this package without going through
// ADR-11's finishing point.
//
// Only one of the three matches a sentinel, and that is a decision rather than an omission:
// errors.go's vocabulary is Step 5's, and `finished` unwraps to a member of that vocabulary or to
// nothing at all, so a sentinel declared here would be one no caller could match anyway. The
// wording is therefore the answer, and it is asserted rather than assumed.

// TestEachLedgerRefusalNamesWhatAnOperatorMustActOn asserts the wording rather than only the
// refusal: an operator told the ledger is unusable and not which version or which file cannot act
// on it.
func TestEachLedgerRefusalNamesWhatAnOperatorMustActOn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		verdict  ledgerVerdict
		recorded []appliedVersion
		at       int
		wantText []string
	}{
		{name: "ahead of the binary names both versions", verdict: ledgerAheadOfBinary, at: 3,
			recorded: []appliedVersion{ledgerRow(1, sumOne), ledgerRow(2, sumTwo),
				ledgerRow(3, sumThree), ledgerRow(4, sumEdited)},
			// The database records 4 and the corpus reaches 3, so both numbers must appear.
			wantText: []string{"4", "3"}},
		{name: "a gap names the version found and the one the run had reached",
			verdict:  ledgerNotAPrefix,
			at:       1,
			recorded: []appliedVersion{ledgerRow(1, sumOne), ledgerRow(3, sumThree)},
			// Row 1 records version 3 where an ascending run from 1 reaches 2.
			wantText: []string{"3", "2"}},
		{name: "an edited file names the migration, the file and both checksums",
			verdict:  ledgerFileChanged,
			at:       1,
			recorded: []appliedVersion{ledgerRow(1, sumOne), ledgerRow(2, sumEdited)},
			wantText: []string{"0002_second.sql", sumEdited, sumTwo}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusal := refusedLedger(tc.verdict, tc.at, tc.recorded, threeMigrations())
			if refusal == nil {
				t.Fatalf("%s produced no refusal", tc.verdict)
			}

			for _, named := range tc.wantText {
				if !strings.Contains(refusal.Error(), named) {
					t.Errorf("the refusal reads %s, which does not name %s", refusal, named)
				}
			}
		})
	}
}

// TestAheadOfTheBinaryIsRefusedAsTheVocabularysOwnSentinel is criterion 14's matchable half. The
// text is pinned by equality against the constructor rather than by substring, so a rewrite that
// dropped one of the two versions fails here.
func TestAheadOfTheBinaryIsRefusedAsTheVocabularysOwnSentinel(t *testing.T) {
	recorded := []appliedVersion{ledgerRow(1, sumOne), ledgerRow(2, sumTwo),
		ledgerRow(3, sumThree), ledgerRow(4, sumEdited)}

	refusal := refusedLedger(ledgerAheadOfBinary, 3, recorded, threeMigrations())
	if !errors.Is(refusal, ErrSchemaAhead) {
		t.Errorf("the refusal %s does not match ErrSchemaAhead, so a caller cannot tell it from any "+
			"other reason the ledger was refused", refusal)
	}
	if want := finished(schemaAhead(4, 3)).Error(); refusal.Error() != want {
		t.Errorf("the refusal reads %s, want %s", refusal, want)
	}
}

// everyLedgerVerdict is the whole vocabulary as a value, so the sweep below quantifies over it
// rather than over a list copied into one assertion.
var everyLedgerVerdict = []ledgerVerdict{
	ledgerUsable, ledgerAheadOfBinary, ledgerNotAPrefix, ledgerFileChanged,
}

// TestNoLedgerVerdictReachesTheRendererAndIsCarriedForwardSilently is the fail-closed half.
// ledgerUsable is included deliberately: the renderer is reached only for a refusal, so being
// handed it is a caller defect, and answering nil there would apply migrations to a ledger the
// caller had just judged unusable.
func TestNoLedgerVerdictReachesTheRendererAndIsCarriedForwardSilently(t *testing.T) {
	if len(everyLedgerVerdict) != 4 {
		t.Fatalf("%d verdicts %v are declared; update this count with the set",
			len(everyLedgerVerdict), everyLedgerVerdict)
	}

	unnamed := ledgerVerdict("a verdict this runner does not declare")
	for _, verdict := range append(everyLedgerVerdict, unnamed) {
		refusal := refusedLedger(verdict, 0, []appliedVersion{ledgerRow(9, sumEdited)}, threeMigrations())
		if refusal == nil {
			t.Errorf("the renderer answered nil for %s, so an unrecognised verdict is carried "+
				"forward as if the ledger were usable", verdict)
			continue
		}
		var elided *elidedError
		if !errors.As(refusal, &elided) {
			t.Errorf("the refusal for %s did not leave through the finishing point (ADR-11)", verdict)
		}
	}
}

// TestTheSearchPathStatementIsWhatD1Specifies pins the one interpolation site's output against D1's
// own text rather than against the rule it obeys, because the statement is dictated. The
// quote-carrying row is what separates PostgreSQL's doubling rule from Go's backslash escaping,
// which would close the identifier early.
func TestTheSearchPathStatementIsWhatD1Specifies(t *testing.T) {
	for _, tc := range []struct{ name, schema, want string }{
		{name: "an ordinary schema", schema: "noty",
			want: `SET LOCAL search_path TO "noty", pg_catalog`},
		{name: "a second schema", schema: "tenant_a",
			want: `SET LOCAL search_path TO "tenant_a", pg_catalog`},
		{name: "a schema carrying a quote", schema: `we"ird`,
			want: `SET LOCAL search_path TO "we""ird", pg_catalog`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, fault := searchPathStatement(tc.schema)

			if fault != IdentifierOK {
				t.Fatalf("searchPathStatement refused %s: it %s", strconv.Quote(tc.schema), fault)
			}
			if got != tc.want {
				t.Errorf("searchPathStatement produced %s, want %s",
					strconv.Quote(got), strconv.Quote(tc.want))
			}
		})
	}

	if _, fault := searchPathStatement(""); fault != IdentifierEmpty {
		t.Errorf("an empty schema was refused as %s, want %s; the statement would otherwise leave "+
			"every object routed by whatever the connection's path already named", fault, IdentifierEmpty)
	}
}

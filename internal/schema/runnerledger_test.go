package schema

import "testing"

// The ledger verdict is pure, so it is asserted without a container: given what a database records
// and what this binary embeds, is the ledger something this corpus can carry forward, and if not,
// which of the three reasons is it. runnerrefusal_test.go asserts what each reason then says to an
// operator.
//
// The order the three are tested in is load-bearing rather than cosmetic, so it has rows of its
// own. The checksum comparison indexes corpus[i] for every recorded row, and it is the two checks
// before it that make that safe: the ahead check gives max(recorded) <= max(corpus), the prefix
// check gives max(recorded) == len(recorded), and versionGapIn gives max(corpus) == len(corpus). A
// case violating each pair at once is what makes a reordering fail here rather than panic in
// production.

const (
	sumOne    = "sum-of-the-first-file"
	sumTwo    = "sum-of-the-second-file"
	sumThree  = "sum-of-the-third-file"
	sumEdited = "sum-of-a-file-edited-after-it-was-applied"
)

// threeMigrations is the corpus every row below is judged against. Three rather than two, so a
// prefix, a whole ledger and a ledger reaching past the end are all expressible.
func threeMigrations() []migration {
	return []migration{
		{version: 1, file: "0001_first.sql", checksum: sumOne},
		{version: 2, file: "0002_second.sql", checksum: sumTwo},
		{version: 3, file: "0003_third.sql", checksum: sumThree},
	}
}

func ledgerRow(version int, checksum string) appliedVersion {
	return appliedVersion{version: version, checksum: checksum}
}

func TestEachReasonALedgerCannotBeCarriedForwardHasItsOwnAnswer(t *testing.T) {
	for _, tc := range []struct {
		name     string
		recorded []appliedVersion
		want     ledgerVerdict
		wantAt   int
	}{
		{name: "an empty ledger", want: ledgerUsable},
		{name: "a ledger holding a prefix of the corpus", want: ledgerUsable,
			recorded: []appliedVersion{ledgerRow(1, sumOne)}},
		// The equality row. Every other usable ledger is strictly below the highest embedded
		// version, so `>` and `>=` answer alike on all of them.
		{name: "a ledger recording exactly the highest embedded version", want: ledgerUsable,
			recorded: []appliedVersion{ledgerRow(1, sumOne), ledgerRow(2, sumTwo), ledgerRow(3, sumThree)}},

		{name: "one version above the corpus", want: ledgerAheadOfBinary, wantAt: 3,
			recorded: []appliedVersion{ledgerRow(1, sumOne), ledgerRow(2, sumTwo),
				ledgerRow(3, sumThree), ledgerRow(4, sumEdited)}},
		{name: "a version far above the corpus", want: ledgerAheadOfBinary, wantAt: 1,
			recorded: []appliedVersion{ledgerRow(1, sumOne), ledgerRow(5, sumEdited)}},
		// The index-bound row: reaching the checksum comparison with this ledger would read
		// corpus[3] on a corpus of three.
		{name: "a ledger longer than the corpus", want: ledgerAheadOfBinary, wantAt: 4,
			recorded: []appliedVersion{ledgerRow(1, sumOne), ledgerRow(2, sumTwo), ledgerRow(3, sumThree),
				ledgerRow(4, sumEdited), ledgerRow(5, sumEdited)}},

		{name: "a version missing below the highest recorded", want: ledgerNotAPrefix, wantAt: 1,
			recorded: []appliedVersion{ledgerRow(1, sumOne), ledgerRow(3, sumThree)}},
		{name: "a ledger that does not start at the first version", want: ledgerNotAPrefix,
			recorded: []appliedVersion{ledgerRow(2, sumTwo)}},
		{name: "one version recorded twice", want: ledgerNotAPrefix, wantAt: 1,
			recorded: []appliedVersion{ledgerRow(1, sumOne), ledgerRow(1, sumOne)}},

		{name: "the first file edited after it was applied", want: ledgerFileChanged,
			recorded: []appliedVersion{ledgerRow(1, sumEdited), ledgerRow(2, sumTwo)}},
		{name: "a later file edited after it was applied", want: ledgerFileChanged, wantAt: 1,
			recorded: []appliedVersion{ledgerRow(1, sumOne), ledgerRow(2, sumEdited)}},

		{name: "ahead of the binary and edited", want: ledgerAheadOfBinary, wantAt: 3,
			recorded: []appliedVersion{ledgerRow(1, sumEdited), ledgerRow(2, sumTwo),
				ledgerRow(3, sumThree), ledgerRow(4, sumEdited)}},
		{name: "ahead of the binary and gapped", want: ledgerAheadOfBinary, wantAt: 1,
			recorded: []appliedVersion{ledgerRow(2, sumTwo), ledgerRow(5, sumEdited)}},
		{name: "gapped and edited", want: ledgerNotAPrefix, wantAt: 1,
			recorded: []appliedVersion{ledgerRow(1, sumEdited), ledgerRow(3, sumThree)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict, at := whyUnusableLedger(tc.recorded, threeMigrations())

			if verdict != tc.want {
				t.Errorf("whyUnusableLedger answered %s, want %s", verdict, tc.want)
			}
			if verdict != ledgerUsable && at != tc.wantAt {
				t.Errorf("it fired on recorded row %d, want row %d", at, tc.wantAt)
			}
		})
	}
}

// TestTheRecordedVersionIsTheMaximumOverTheLedgerRatherThanItsRowCount pins ADR-5's definition on
// the one input that separates the two readings, since for every well-formed prefix they agree.
func TestTheRecordedVersionIsTheMaximumOverTheLedgerRatherThanItsRowCount(t *testing.T) {
	gapped := []appliedVersion{ledgerRow(1, sumOne), ledgerRow(5, sumEdited)}

	if got := highestRecorded(gapped); got != 5 {
		t.Errorf("highestRecorded answered %d for a ledger recording 1 and 5, want 5", got)
	}
	if got := highestRecorded(nil); got != 0 {
		t.Errorf("highestRecorded answered %d for a database recording nothing, want 0", got)
	}
	if got := highestEmbedded(nil); got != 0 {
		t.Errorf("highestEmbedded answered %d for a corpus carrying nothing, want 0", got)
	}
	if got := highestEmbedded(threeMigrations()); got != 3 {
		t.Errorf("highestEmbedded answered %d for a corpus of three, want 3", got)
	}
}

package source

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
)

// The absence half of the payload-cap claim. Enforcing payload.max_bytes belongs to
// internal/delivery, so what this package owes is generated DDL carrying no truncation, no length
// test and no reference to the cap at all.
//
// An absence claim is only worth what its enumeration and its control are worth: a scanner that
// finds nothing anywhere passes an absence scan silently. The enumeration below is the set of
// constructs an enforcing generator would emit, every one of them has a synthetic violation the
// same scanner is driven over, and the enumeration is closed against those rows so a needle added
// without a row fails here (modelled on quotingscan_test.go's ::text[] absence scan and its
// control).

// theScannedCap is the cap the generator is handed. It is a number no derived name, dollar-quote
// tag or marker in the generated text can carry, so "the cap's own digits are absent" is a claim
// about the cap rather than a coincidence.
const theScannedCap = 1234567

// payloadCapNeedles is what an enforcing generator would have to write. Each is matched against the
// lower-cased statement, so a capitalised spelling is caught too.
var payloadCapNeedles = []string{
	"octet_length", "char_length", "bit_length", "pg_column_size",
	"length(", "left(", "substr", "truncate", "max_bytes",
}

// payloadCapViolations is one synthetic statement per needle: what the generator would look like if
// it had enforced the cap here. Each row names the needle it is written for, so a row that stops
// reaching its own clause fails rather than being covered by a neighbour.
var payloadCapViolations = []struct{ name, written, needle string }{
	{"a byte-length test", "IF octet_length(payload::text) > 1234567 THEN RETURN NULL; END IF;", "octet_length"},
	{"a character-length test", "IF char_length(payload::text) > 1234567 THEN RETURN NULL; END IF;", "char_length"},
	{"a bit-length test", "IF bit_length(payload::text) > 9876543 THEN RETURN NULL; END IF;", "bit_length"},
	{"a stored-size test", "IF pg_column_size(payload) > 9876543 THEN RETURN NULL; END IF;", "pg_column_size"},
	{"a bare length test", "IF LENGTH(payload::text) > 9876543 THEN RETURN NULL; END IF;", "length("},
	{"a left truncation", "payload := left(payload::text, 9876543)::jsonb;", "left("},
	{"a substring truncation", "payload := substr(payload::text, 1, 9876543)::jsonb;", "substr"},
	{"a named truncation", "PERFORM noty.truncate_payload(payload);", "truncate"},
	{"a reference to the configured cap", "-- capped at the listener's max_bytes", "max_bytes"},
}

// theCapIsNeverWritten is the near-miss the scan must not report: a statement naming the payload and
// carrying digits, in the shape the generator actually emits.
const theCapIsNeverWritten = "jsonb_build_object('id', NEW.\"id\", 'n', 7654321)"

// generatedStatements is the DDL text of one object set, in the order internal/reconcile applies
// it. It is the one such list: injection_integration_test.go's executeObjectSet applies these five, in this
// order, by calling this rather than by restating it.
func generatedStatements(set ObjectSet) []string {
	return []string{set.CreateFunction, set.RevokeExecute, set.CommentFunction, set.CreateTrigger, set.CommentTrigger}
}

// payloadCapIssues reports every cap-enforcing construct the statements carry, including the cap's
// own digits, which no generator can write without having read the cap.
func payloadCapIssues(statements []string, configured int) []string {
	var issues []string
	for _, needle := range append(slices.Clone(payloadCapNeedles), strconv.Itoa(configured)) {
		for _, statement := range statements {
			if strings.Contains(strings.ToLower(statement), needle) {
				issues = append(issues, "the generated DDL writes "+needle+" in "+statement)
			}
		}
	}
	return issues
}

// cappedPayloadModes is the payload grid the absence claim ranges over: enforcement could have been
// written into any mode's expression, so one mode proves nothing about the others.
var cappedPayloadModes = []config.Payload{
	{Mode: "full", MaxBytes: theScannedCap},
	{Mode: "full", Exclude: []string{"secret"}, MaxBytes: theScannedCap},
	{Mode: "columns", Columns: []string{"status"}, MaxBytes: theScannedCap},
	{Mode: "keys_only", MaxBytes: theScannedCap},
}

func TestNoGeneratedStatementTestsOrTruncatesThePayloadLength(t *testing.T) {
	for _, payload := range cappedPayloadModes {
		for _, kind := range []string{"insert", "update", "delete"} {
			request := generationRequest(config.Operation{Kind: kind})
			request.Listener.Trigger.Payload = payload
			sets, err := Generate(request)
			if err != nil {
				t.Fatalf("%s/%s: %v", payload.Mode, kind, err)
			}
			for _, issue := range payloadCapIssues(generatedStatements(sets[0]), theScannedCap) {
				t.Errorf(
					"%s/%s: %s; enforcing payload.max_bytes belongs to internal/delivery, and "+
						"the oversized event is recorded in full", payload.Mode, kind, issue,
				)
			}
		}
	}
}

func TestThePayloadCapScanReportsEverySyntheticViolationAndSparesTheNearMiss(t *testing.T) {
	for _, violation := range payloadCapViolations {
		t.Run(
			violation.name, func(t *testing.T) {
				issues := payloadCapIssues([]string{violation.written}, theScannedCap)
				if !slices.ContainsFunc(
					issues, func(issue string) bool {
						return strings.Contains(issue, "writes "+violation.needle+" ")
					},
				) {
					t.Fatalf(
						"the scan reported %v for %s, and none of them names %s",
						issues, violation.written, violation.needle,
					)
				}
			},
		)
	}
	if issues := payloadCapIssues([]string{theCapIsNeverWritten}, theScannedCap); len(issues) != 0 {
		t.Errorf("the scan reported %v for a statement carrying only the shape the generator emits", issues)
	}
	if issues := payloadCapIssues(
		[]string{"-- honours " + strconv.Itoa(theScannedCap)},
		theScannedCap,
	); len(issues) == 0 {
		t.Error("the scan spared a statement writing the configured cap's own digits")
	}
}

func TestEveryPayloadCapNeedleHasItsOwnSyntheticViolation(t *testing.T) {
	written := make(map[string]int, len(payloadCapViolations))
	for _, violation := range payloadCapViolations {
		written[violation.needle]++
	}
	for _, needle := range payloadCapNeedles {
		if written[needle] == 0 {
			t.Errorf(
				"no synthetic violation is written for %s, so the scan's clause for it is never "+
					"driven", needle,
			)
		}
	}
	if len(payloadCapViolations) != len(payloadCapNeedles) {
		t.Fatalf(
			"%d synthetic violations for %d needles; add the row with the needle",
			len(payloadCapViolations), len(payloadCapNeedles),
		)
	}
}

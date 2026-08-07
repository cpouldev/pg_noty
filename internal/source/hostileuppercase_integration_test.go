//go:build integration

package source

import (
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The uppercase class at the two configuration positions
// TestUppercaseConfiguredColumnSelectsExactColumn does not reach. That case covers payload.columns,
// which renders the name through *both* authorities -- quoteLiteral for the JSON key, schema.Quoted
// for the identifier -- so dropping either fails it. The positions here render it once each and
// through a different one: UPDATE OF is identifier-only, and exclude produces no identifier at all,
// only a literal that has to byte-match a to_jsonb key. Their failures are ones neither other
// position can produce: an OF list watching the column nobody configured, and a subtraction removing
// the column nobody excluded. The forms below are written out rather than taken from the authorities
// under test, and the table carries both spellings so that a folded rendering selects an existing
// column rather than erroring.
const (
	theUppercaseTable    = "uppercase_target"
	theUppercaseName     = "Status"
	theFoldedName        = "status"
	theUppercaseQuoted   = `"Status"`
	theUppercaseLiteral  = `'Status'`
	theFoldedQuoted      = `"status"`
	theFoldedLiteral     = `'status'`
	createUppercaseTable = "CREATE TABLE public." + theUppercaseTable +
		" (" + theUppercaseQuoted + " text, " + theFoldedName + " text, id int)"
	insertUppercaseRow = "INSERT INTO public." + theUppercaseTable +
		" (" + theUppercaseQuoted + ", " + theFoldedName + ", id) VALUES ('UP', 'low', 1)"
)

// Declared once, so a control cannot differ from its case in anything but the quoting it removes.
var (
	theUppercaseUpdate  = config.Operation{Kind: "update", Columns: []string{theUppercaseName}}
	theUppercaseExclude = config.Payload{Mode: "full", Exclude: []string{theUppercaseName}}
)

func freshUppercaseTarget(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := freshDatabase(t)
	applySourceMigrations(t, pool)
	mustExecOn(t, pool, createUppercaseTable)
	return pool
}

// uppercaseSet renders one position through the generator's own entry point, once, for both tiers.
func uppercaseSet(t *testing.T, operation config.Operation, payload config.Payload) ObjectSet {
	t.Helper()
	request := generationRequest(operation)
	request.Target.Table = theUppercaseTable
	request.Listener.Trigger.Payload = payload
	sets, err := Generate(request)
	if err != nil || len(sets) != 1 {
		t.Fatalf("the uppercase request produced %d object sets: %v", len(sets), err)
	}
	return sets[0]
}

// rewritten removes one authority's rendering, and fails when the substitution matched nothing: a
// control that changed no byte runs the authority's own text and then passes for the wrong reason.
func rewritten(t *testing.T, statement, from, to string) string {
	t.Helper()
	replaced := strings.Replace(statement, from, to, 1)
	if replaced == statement {
		t.Fatalf("the control replaced no %s, so it renders the authority's own text:\n%s", from, statement)
	}
	return replaced
}

// firstRefusal returns the first statement the server refused: CREATE FUNCTION or the row write.
func firstRefusal(t *testing.T, pool *pgxpool.Pool, set ObjectSet) error {
	t.Helper()
	for _, statement := range append(generatedStatements(set), insertUppercaseRow) {
		if _, err := pool.Exec(t.Context(), statement); err != nil {
			return err
		}
	}
	return nil
}

// assertUpdateOfSelects writes each spelling of the collided name in turn and reads the event log
// after each, so a case and its control differ only in the counts. Both sides are asserted: an OF
// list folded -- or dropped -- fires on the configured column too.
func assertUpdateOfSelects(t *testing.T, set ObjectSet, afterFolded, afterConfigured int) {
	t.Helper()
	pool := freshUppercaseTarget(t)
	mustExecOn(t, pool, insertUppercaseRow)
	executeObjectSet(t, pool, set)
	for _, side := range []struct {
		column, value string
		want          int
	}{{theFoldedQuoted, "low2", afterFolded}, {theUppercaseQuoted, "UP2", afterConfigured}} {
		mustExecOn(t, pool, "UPDATE public."+theUppercaseTable+" SET "+side.column+" = $1 WHERE id=1", side.value)
		if got := rowCountOf(t, pool, "noty.events"); got != side.want {
			t.Fatalf("writing %s left %d events in the log, want %d", side.column, got, side.want)
		}
	}
}

// assertPayloadKeys reads the keys back byte-exactly, binding each as a query parameter.
func assertPayloadKeys(t *testing.T, pool *pgxpool.Pool, removed string, kept ...string) {
	t.Helper()
	if payloadHasKey(t, pool, removed) {
		t.Errorf("the stored payload still carries %s, the key the subtraction names", removed)
	}
	for _, key := range kept {
		if !payloadHasKey(t, pool, key) {
			t.Errorf("the stored payload lost %s, which the subtraction does not name", key)
		}
	}
}

// TestUppercaseUpdateOfColumnFiltersOnTheExactColumn is the UPDATE OF position: schema.Quoted alone.
func TestUppercaseUpdateOfColumnFiltersOnTheExactColumn(t *testing.T) {
	skipIfShort(t)
	set := uppercaseSet(t, theUppercaseUpdate, config.Payload{Mode: "full"})
	if want := "UPDATE OF " + theUppercaseQuoted; !strings.Contains(set.CreateTrigger, want) {
		t.Fatalf("CreateTrigger does not carry %s:\n%s", want, set.CreateTrigger)
	}
	for _, folded := range []string{"UPDATE OF " + theFoldedName, "UPDATE OF " + theFoldedQuoted} {
		if strings.Contains(set.CreateTrigger, folded) {
			t.Errorf("CreateTrigger carries %s, a spelling the server folds to:\n%s", folded, set.CreateTrigger)
		}
	}
	// Empty log after the folded column, one row after the configured one: silence alone is no proof.
	assertUpdateOfSelects(t, set, 0, 1)
}

// TestWithoutTheQuotingAuthorityTheUppercaseUpdateFilterSelectsTheWrongColumn renders the same name
// into the same statement with schema.Quoted's quoting removed. The server folds the bare spelling,
// so the trigger watches `status` -- a column nobody configured -- rather than failing, which is why
// the case above reads *which* column fired rather than that one did.
func TestWithoutTheQuotingAuthorityTheUppercaseUpdateFilterSelectsTheWrongColumn(t *testing.T) {
	skipIfShort(t)
	t.Run(
		"WithoutTheQuotingAuthority the update filter selects the wrong column", func(t *testing.T) {
			set := uppercaseSet(t, theUppercaseUpdate, config.Payload{Mode: "full"})
			set.CreateTrigger = rewritten(
				t,
				set.CreateTrigger,
				"UPDATE OF "+theUppercaseQuoted,
				"UPDATE OF "+theUppercaseName,
			)
			// The first write fires; the second, of the column actually configured, adds nothing.
			assertUpdateOfSelects(t, set, 1, 1)
		},
	)
}

// TestUppercaseExcludedColumnIsSubtractedByItsExactKey is the exclude position: quoteLiteral alone.
// `to_jsonb(NEW) - 'Status'` removes a jsonb key, and a jsonb key matches byte for byte. Counting
// subtractions cannot see that -- a folded operand subtracts as many times -- so the keys are read
// back rather than inferred from the emitted text.
func TestUppercaseExcludedColumnIsSubtractedByItsExactKey(t *testing.T) {
	skipIfShort(t)
	set := uppercaseSet(t, config.Operation{Kind: "insert"}, theUppercaseExclude)
	if want := " - " + theUppercaseLiteral; !strings.Contains(set.CreateFunction, want) {
		t.Fatalf("CreateFunction does not carry %s:\n%s", want, set.CreateFunction)
	}
	for _, wrong := range []string{" - " + theFoldedLiteral, " - " + theUppercaseQuoted, " - " + theFoldedQuoted} {
		if strings.Contains(set.CreateFunction, wrong) {
			t.Errorf("CreateFunction carries %s, not the name literal-quoted:\n%s", wrong, set.CreateFunction)
		}
	}
	pool := freshUppercaseTarget(t)
	executeObjectSet(t, pool, set)
	mustExecOn(t, pool, insertUppercaseRow)
	assertPayloadKeys(t, pool, theUppercaseName, theFoldedName, "id")
}

// theExcludeControlRenderings are the two ways the name reaches the subtraction once the literal
// authority is removed, and they fail differently, so neither stands for the other: the folded key
// is legal and removes the column nobody excluded, while the identifier quoting the other two
// positions need is a column reference here, because a subtraction operand is a value.
var theExcludeControlRenderings = []struct {
	name, rendered string
	wantRefusal    bool
}{
	{name: "WithoutTheQuotingAuthority the folded key removes the other column", rendered: theFoldedLiteral},
	{
		name: "WithoutTheQuotingAuthority the identifier quoting is refused", rendered: theUppercaseQuoted,
		wantRefusal: true,
	},
}

func TestWithoutTheQuotingAuthorityTheUppercaseExcludeSubtractionMissesItsColumn(t *testing.T) {
	skipIfShort(t)
	for _, control := range theExcludeControlRenderings {
		t.Run(
			control.name, func(t *testing.T) {
				set := uppercaseSet(t, config.Operation{Kind: "insert"}, theUppercaseExclude)
				set.CreateFunction = rewritten(t, set.CreateFunction, " - "+theUppercaseLiteral, " - "+control.rendered)
				pool := freshUppercaseTarget(t)
				refusal := firstRefusal(t, pool, set)
				if (refusal != nil) != control.wantRefusal {
					t.Fatalf(
						"the %s rendering answered %v, want refused=%t",
						control.rendered,
						refusal,
						control.wantRefusal,
					)
				}
				if !control.wantRefusal {
					assertPayloadKeys(t, pool, theFoldedName, theUppercaseName, "id")
				}
			},
		)
	}
}

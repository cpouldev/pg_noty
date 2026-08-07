package schema

import (
	"strings"
	"testing"
)

// The Security NFR's own rows, at the level a server reads them: not "the function returned
// something" but "the text is one statement naming exactly the configured objects". Both positions
// the criterion names are covered -- a hostile schema name and a hostile table name -- because the
// quoting of one says nothing about the other.

// hostileSchema and hostileTable each carry a double quote, a statement separator and a comment
// sequence, which are the three metacharacters that turn an unquoted identifier into code.
const (
	hostileSchema = `no"ty; DROP TABLE orders; --`
	hostileTable  = `ord"ers; DROP SCHEMA noty CASCADE; --`
	// The target is schema-qualified as configuration writes it, and its schema part is hostile too.
	hostileTargetSchema = `pub"lic`
	hostileTarget       = hostileTargetSchema + "." + hostileTable
)

func TestAHostileSchemaAndTableRenderAsOneInertStatementEach(t *testing.T) {
	statements := GrantStatements(hostileSchema, fixtureRole, []string{hostileTarget})
	if len(statements) != 4 {
		t.Fatalf("a hostile configuration emitted %d statements %q, want the four the set holds",
			len(statements), statements)
	}

	for _, tc := range []struct {
		family    grantFamily
		statement string
		wantNamed []string
	}{
		{family: familyOwnership, statement: statements[0], wantNamed: []string{hostileSchema, fixtureRole}},
		{family: familyCreate, statement: statements[1], wantNamed: []string{hostileSchema, fixtureRole}},
		{family: familyUsage, statement: statements[2], wantNamed: []string{hostileSchema, fixtureRole}},
		{family: familyTrigger, statement: statements[3],
			wantNamed: []string{hostileTargetSchema, hostileTable, fixtureRole}},
	} {
		t.Run(string(tc.family), func(t *testing.T) {
			if familyOf(tc.statement) != tc.family {
				t.Fatalf("%s is %s, want %s", tc.statement, familyOf(tc.statement), tc.family)
			}
			assertOneInertStatement(t, tc.statement, tc.wantNamed)
		})
	}
}

// assertOneInertStatement reads a statement the way a server reads it and asserts three things: it
// names exactly the objects it was given, every metacharacter those names carry sits inside a
// delimited identifier, and what is left outside them is one statement.
func assertOneInertStatement(t *testing.T, statement string, wantNamed []string) {
	t.Helper()

	named, outside, closed := statementShape(statement)
	if !closed {
		t.Fatalf("%s leaves a delimited identifier open, so everything after it is a name", statement)
	}
	if strings.Join(named, "\x00") != strings.Join(wantNamed, "\x00") {
		t.Errorf("%s names %q, want %q -- it addresses different objects", statement, named, wantNamed)
	}

	if separators := strings.Count(outside, ";"); separators != 1 {
		t.Errorf("%s carries %d statement separators outside its identifiers, want 1: %q",
			statement, separators, outside)
	}
	if !strings.HasSuffix(outside, ";") {
		t.Errorf("%s has text after its separator, so it is two statements: %q", statement, outside)
	}
	if strings.Contains(outside, "--") {
		t.Errorf("%s carries a comment sequence outside its identifiers, so the rest of the line "+
			"is commented out: %q", statement, outside)
	}
}

// statementShape reads one statement the way a server reads it: the delimited identifiers it names,
// with a doubled quote inside one read as a single quote character, and the text left outside them.
// It answers closed = false for a statement that opens an identifier and never closes it, because
// then there is no "outside" to judge.
func statementShape(statement string) (named []string, outside string, closed bool) {
	var plain strings.Builder
	for text := statement; text != ""; {
		if text[0] != '"' {
			plain.WriteByte(text[0])
			text = text[1:]
			continue
		}
		part, tail, delimited := readDelimitedPart(text)
		if !delimited {
			return nil, "", false
		}
		named = append(named, part)
		text = tail
	}
	return named, plain.String(), true
}

// TestTheStatementOracleTellsAnInertStatementFromAnEscapedOne gives the oracle its own
// falsifiability: without these rows an oracle that answered "inert" for everything would make the
// rows above pass. Each expectation is counted off the statement's own bytes rather than recorded
// from a run.
func TestTheStatementOracleTellsAnInertStatementFromAnEscapedOne(t *testing.T) {
	for _, tc := range []struct {
		name, statement string
		wantNamed       []string
		wantOutside     string
		wantClosed      bool
	}{
		{
			name:        "a separator carried inside the identifier",
			statement:   `GRANT USAGE ON SCHEMA "noty;drop" TO "pg_noty";`,
			wantNamed:   []string{"noty;drop", "pg_noty"},
			wantOutside: "GRANT USAGE ON SCHEMA  TO ;",
			wantClosed:  true,
		},
		{
			name:        "a doubled quote read back as one quote",
			statement:   `ALTER SCHEMA "no""ty" OWNER TO "pg_noty";`,
			wantNamed:   []string{`no"ty`, "pg_noty"},
			wantOutside: "ALTER SCHEMA  OWNER TO ;",
			wantClosed:  true,
		},
		{
			// What a quoter that wrapped without doubling emits: two separators outside.
			name:        "an identifier closed early, leaving a second statement",
			statement:   `ALTER SCHEMA "no"; DROP TABLE orders OWNER TO "pg_noty";`,
			wantNamed:   []string{"no", "pg_noty"},
			wantOutside: "ALTER SCHEMA ; DROP TABLE orders OWNER TO ;",
			wantClosed:  true,
		},
		{
			name:        "a comment sequence outside the identifiers",
			statement:   `ALTER SCHEMA "noty" -- x OWNER TO "pg_noty";`,
			wantNamed:   []string{"noty", "pg_noty"},
			wantOutside: "ALTER SCHEMA  -- x OWNER TO ;",
			wantClosed:  true,
		},
		{
			name:       "an identifier never closed",
			statement:  `ALTER SCHEMA "noty OWNER TO pg_noty;`,
			wantClosed: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			named, outside, closed := statementShape(tc.statement)

			if closed != tc.wantClosed {
				t.Fatalf("statementShape read closed = %t, want %t", closed, tc.wantClosed)
			}
			if !closed {
				return
			}
			if strings.Join(named, "\x00") != strings.Join(tc.wantNamed, "\x00") || outside != tc.wantOutside {
				t.Errorf("statementShape read %q outside %q, want %q outside %q",
					named, outside, tc.wantNamed, tc.wantOutside)
			}
		})
	}
}

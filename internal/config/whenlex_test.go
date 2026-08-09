package config

import (
	goparser "go/parser"
	gotoken "go/token"
	"strings"
	"testing"
)

// longTag and overlongTag are dollar-quote tags at and one byte past NAMEDATALEN - 1, the
// bound identifierDefect applies to an object name. A tag is not an object name and
// PostgreSQL's tag production carries no length at all, so both must open a literal; the pair
// is what makes the absence of that bound observable rather than assumed.
var (
	longTag     = strings.Repeat("t", maxIdentifierBytes)
	overlongTag = strings.Repeat("t", maxIdentifierBytes+1)
)

// dollarQuotedClause returns a clause whose only OLD sits inside a dollar-quoted literal
// tagged with tag. A row built from it expects silence, so it fails unless the tag opened a
// literal at all: a tag the scanner refuses leaves that OLD in code, where it is reported.
func dollarQuotedClause(tag string) string {
	delimiter := "$" + tag + "$"
	return "note = " + delimiter + " OLD " + delimiter
}

// whenClauseCases is the one table this file's scanner cases live in, so that the detecting
// and the non-detecting halves stay visibly balanced: a scanner that never fires would
// satisfy every adversarial case perfectly, and one that fires on any `OLD` would satisfy
// every detecting case.
//
// It is also the fuzz seed corpus (FuzzWhenScannerReportsARealWord), so exploration starts
// from AC #18's six adversarial cases rather than from nothing.
//
// Every wantStart is counted from the clause and the count is shown, because an index is a
// value a wrong answer looks exactly like a right one
// (.claude/rules/derive-expected-values.md).
var whenClauseCases = []struct {
	name      string
	operation string
	clause    string
	// wantWord is the row variable expected in the finding, and empty when no finding is
	// expected at all.
	wantWord  string
	wantStart int
}{
	{
		name:      "OLD under insert",
		operation: "insert",
		clause:    "OLD.status = 'x'",
		wantWord:  "OLD",
		wantStart: 0,
	},
	{
		name:      "NEW under delete",
		operation: "delete",
		clause:    "NEW.status = 'x'",
		wantWord:  "NEW",
		wantStart: 0,
	},
	{
		// The finding names the row variable as PostgreSQL spells it in its refusal, not
		// as the author wrote it.
		name:      "lowercase old under insert",
		operation: "insert",
		clause:    "old.status = 'x'",
		wantWord:  "OLD",
		wantStart: 0,
	},
	{
		// t0 .1 x2 sp3 =4 sp5 16 sp7 A8 N9 D10 sp11 -- so OLD begins at byte 12. A row whose
		// reference sits at the start would pass against a hard-coded zero
		// (.claude/rules/partition-named-test-cases.md).
		name:      "OLD after a leading condition",
		operation: "insert",
		clause:    "t.x = 1 AND OLD.status <> 'paid'",
		wantWord:  "OLD",
		wantStart: 12,
	},
	{
		// o0 l1 d2 .3 t4 o5 t6 a7 l8 sp9 >10 sp11 -- new begins at byte 12. OLD is legal
		// under delete, so this row also proves only the forbidden variable is reported.
		name:      "the permitted variable precedes the forbidden one",
		operation: "delete",
		clause:    "old.total > new.total",
		wantWord:  "NEW",
		wantStart: 12,
	},
	{
		name:      "NEW under insert is permitted",
		operation: "insert",
		clause:    "NEW.status = 'paid'",
	},
	{
		name:      "OLD under delete is permitted",
		operation: "delete",
		clause:    "OLD.status = 'paid'",
	},
	{
		name:      "both variables under update are permitted",
		operation: "update",
		clause:    "OLD.status <> NEW.status",
	},
	{
		// An operation outside the three is R28's to reject; nothing is forbidden for it
		// here, so this scanner stays silent rather than guessing.
		name:      "an operation that is not one of the three",
		operation: "truncate",
		clause:    "OLD.status = 'x'",
	},

	// AC #18's six adversarial cases. Each holds the text OLD somewhere the scanner must
	// not look, and each must produce no finding.
	{
		name:      "OLD inside a single-quoted literal",
		operation: "insert",
		clause:    "status = 'OLD'",
	},
	{
		name:      "OLD inside a dollar-quoted literal",
		operation: "insert",
		clause:    "status = $$OLD$$",
	},
	{
		name:      "OLD inside a line comment",
		operation: "insert",
		clause:    "status = 'paid' -- OLD is not referenced",
	},
	{
		name:      "OLD inside a block comment",
		operation: "insert",
		clause:    "status = 'paid' /* OLD is not referenced */",
	},
	{
		name:      "OLD inside a double-quoted identifier",
		operation: "insert",
		clause:    `"OLD" = 1`,
	},
	{
		name:      "OLD as the head of a longer word",
		operation: "insert",
		clause:    "old_price > 0",
	},

	// Dollar quoting, comment nesting and word boundaries.
	{
		name:      "untagged dollar quote",
		operation: "insert",
		clause:    "note = $$ OLD $$",
	},
	{
		name:      "tagged dollar quote",
		operation: "insert",
		clause:    "note = $tag$ OLD $tag$",
	},
	{
		// A scan that closed at the next `$...$` would close at $inner$ and read ` OLD `
		// as code. The literal ends only at its own tag.
		name:      "a non-matching inner tag does not terminate the literal",
		operation: "insert",
		clause:    "note = $outer$ $inner$ OLD $outer$",
	},
	{
		// PostgreSQL nests block comments, unlike C. A scanner that stopped at the first
		// `*/` would read ` OLD */ status = 1` as code.
		name:      "nested block comments",
		operation: "insert",
		clause:    "/* outer /* inner */ OLD */ status = 1",
	},
	{
		// `/*` and `*/` share the `*`, and a comment cannot be closed by its own opener: this
		// is unterminated for the server, so ` OLD` is inside it. A scan that stepped one byte
		// past the opener would offer that `*` to the closing test, end the comment at byte 3
		// and report the OLD after it.
		name:      "a block comment opener's own asterisk does not close it",
		operation: "insert",
		clause:    "/*/ OLD",
	},
	{
		// The closing token's other byte, asserted the same way round: a comment abutting a
		// `*` operator. Ending the comment one byte early would strand its `/`, which the next
		// `*` then joins into a fresh `/*` opener -- swallowing the rest of the clause and
		// missing a real reference, the failure direction that lets PostgreSQL refuse the DDL.
		//
		// q0 t1 y2 sp3 /4 *5 sp6 p7 c8 s9 sp10 *11 /12 *13 sp14 215 sp16 >17 sp18 -- so OLD
		// begins at byte 19.
		name:      "a closed block comment abutting an asterisk ends where it should",
		operation: "insert",
		clause:    "qty /* pcs */* 2 > OLD.qty",
		wantWord:  "OLD",
		wantStart: 19,
	},
	{
		name:      "newest is not the NEW variable",
		operation: "delete",
		clause:    "newest > 1",
	},
	{
		name:      "older is not the OLD variable",
		operation: "insert",
		clause:    "older < 2",
	},
	{
		// A dollar sign continues an identifier, so this is one word rather than `old`
		// followed by a dollar-quoted literal.
		name:      "OLD as the head of a word containing a dollar sign",
		operation: "insert",
		clause:    "old$price > 0",
	},
	{
		// ident_start admits every byte from \200 up, so `€old` is one identifier to the server
		// and no reference to its OLD row. A charset that admitted only the bytes above ASCII
		// that decode to *letters* rejects `€` as a word opener, advances a byte at a time
		// through its three, and reports the `old` at byte 3 -- a false positive on a clause
		// PostgreSQL accepts.
		name:      "a high byte before the variable is part of the same word",
		operation: "insert",
		clause:    "€old > 0",
	},
	{
		// The other side of that widening: a word holding high bytes still ends at the first
		// byte that cannot continue one, so the reference after it is still reached. A charset
		// that let a high byte swallow the rest of the clause would satisfy the row above while
		// missing every reference past one -- the failure direction that lets PostgreSQL refuse
		// the DDL instead.
		//
		// t0 o1 t2 a3 l4 and € at 5-7 (three bytes), sp8 >9 sp10 -- so OLD begins at byte 11.
		name:      "a word holding a high byte still ends where it must",
		operation: "insert",
		clause:    "total€ > OLD.total",
		wantWord:  "OLD",
		wantStart: 11,
	},

	// The same spelling is not always the row variable: after a field selector it names a column
	// of the row, and an INSERT trigger's WHEN condition may reference one. Measured on
	// PostgreSQL 18.4 -- `WHEN (NEW.old > 1)` is accepted against a table with a column named
	// `old`, and stored as `new.old > 1`, while `WHEN ((OLD).label IS NOT NULL)` is refused with
	// "INSERT trigger's WHEN condition cannot reference OLD values". So the exclusion is a
	// position and not a context, and both sides of it are rows here
	// (.claude/rules/same-spelling-is-not-the-same-construct.md,
	// .claude/rules/test-both-sides-of-an-exclusion-guard.md).
	{
		// Realistic rather than exotic: an audit or diff table carries `old`/`new` columns, and
		// the author of a `when` clause over one has no workaround short of deleting the clause.
		name:      "the forbidden spelling as a field name of the permitted row",
		operation: "insert",
		clause:    "NEW.old > 1",
	},
	{
		name:      "the forbidden spelling as a field name, under delete",
		operation: "delete",
		clause:    "OLD.new > 1",
	},
	{
		// The three spellings of the gap around a selector, one row each because each is one arm
		// of the separator rule: dropping an arm reports the field name that follows it. The
		// server accepts all three (`NEW . old`, `NEW./* pick */old`, `NEW.-- pick<LF>qty`).
		name:      "whitespace around the field selector still selects a field",
		operation: "insert",
		clause:    "NEW . old > 1",
	},
	{
		name:      "a block comment around the field selector still selects a field",
		operation: "insert",
		clause:    "NEW./* pick */old > 1",
	},
	{
		name:      "a line comment after the field selector still selects a field",
		operation: "insert",
		clause:    "NEW.-- pick\nold > 1",
	},
	{
		// The nearest position that must still fire: `(OLD)` is the row itself and the selector
		// after it selects from that, which the server refuses. `(` at 0, so OLD begins at 1.
		name:      "the row variable in parentheses is still a reference",
		operation: "insert",
		clause:    "(OLD).status IS NOT NULL",
		wantWord:  "OLD",
		wantStart: 1,
	},
	{
		// The exclusion covers one position and not the rest of the clause -- the server refuses
		// this clause for its second half. A scanner that stayed in the excluded state would
		// satisfy every row above while missing a reference PostgreSQL rejects the DDL for.
		//
		// N0 E1 W2 .3 o4 l5 d6 sp7 >8 sp9 110 sp11 A12 N13 D14 sp15 -- so OLD begins at 16.
		name:      "a field name of the permitted row does not hide the reference after it",
		operation: "insert",
		clause:    "NEW.old > 1 AND OLD.qty > 2",
		wantWord:  "OLD",
		wantStart: 16,
	},
	{
		// A backslash escapes the quote in an E'...' literal, so the literal runs to the
		// final quote and OLD is inside it. A scanner that ignored the escape would close
		// at the escaped quote and read `s OLD news` as code.
		name:      "OLD inside an escape-string literal past a backslash-escaped quote",
		operation: "insert",
		clause:    `note = E'it\'s OLD news'`,
	},
	{
		// The other side of that guard: standard_conforming_strings has been on by
		// default since PostgreSQL 9.1, so a backslash in a plain literal is an ordinary
		// character and the literal ends at the next quote. n0 o1 t2 e3 sp4 =5 sp6 '7 a8
		// \9 '10 sp11 O12 R13 sp14 -- so OLD begins at byte 15
		// (.claude/rules/test-both-sides-of-an-exclusion-guard.md).
		name:      "a backslash does not escape in a plain literal",
		operation: "insert",
		clause:    `note = 'a\' OR OLD.status = 'x'`,
		wantWord:  "OLD",
		wantStart: 15,
	},

	// Doubling the quote is PostgreSQL's other way of writing one inside a literal or a quoted
	// identifier, and it is content under every quoting rule this scanner applies -- scan.l's
	// xqdouble production runs in the escape-string state as well as the plain one, and xddouble
	// does the same inside a quoted identifier. There is a row per rule because the naive reading
	// -- close the literal at the first quote of the pair, reopen it at the second -- was once
	// assumed to agree with this one for every input, and it agrees only under plain quoting
	// (.claude/rules/pin-a-claimed-equivalence.md). Which row can fail on what was then measured
	// with mutations, so the scoping is evidence rather than argument:
	//
	//   - Under plain quoting the first two rows cannot fail on the naive reading, and restricting
	//     the pair to backslash-escaped literals really does leave the whole suite green: that is
	//     what the equivalence means, measured. They do fail on any other mishandling of the pair --
	//     ending the literal after it kills exactly these two rows and nothing else in the suite.
	//   - Under backslash escaping the two readings diverge, and the third row fails on the naive
	//     one -- as does a seed of FuzzWhenScannerFindsNothingInsideNonCode, which is the assertion
	//     that would have caught this without anyone having to name the case.
	{
		// n0 o1 t2 e3 sp4 =5 sp6 '7 a8 '9 '10 sp11 -- the pair at 9 is one quote of content, so
		// the literal runs to the quote at 16 and every byte between is inside it.
		name:      "a doubled quote inside a plain literal is content",
		operation: "insert",
		clause:    "note = 'a'' OLD '",
	},
	{
		name:      "a doubled quote inside a quoted identifier is content",
		operation: "insert",
		clause:    `"a"" OLD " = 1`,
	},
	{
		// `SELECT E'a''\' OLD ';` returns the string `a'' OLD `, so the whole value is one
		// string constant. A scanner that closed at the first quote of the pair reopens the
		// remainder under plain quoting, where the backslash at 12 is inert, closes again at 13
		// and reports the OLD at 15 -- a false positive on a clause the server accepts.
		name:      "a doubled quote inside an escape-string literal is content",
		operation: "insert",
		clause:    `note = E'a''\' OLD '`,
	},
	{
		// The other side of the E guard: E is only a literal prefix when a quote follows it,
		// and here one never does. A scanner that took the word for a prefix without checking
		// would scan from byte 8 for a closing quote, find none, swallow the rest of the
		// clause and miss the reference entirely.
		//
		// n0 o1 t2 e3 sp4 =5 sp6 E7 sp8 A9 N10 D11 sp12 -- so OLD begins at byte 13.
		name:      "a bare E is a word rather than a literal prefix",
		operation: "insert",
		clause:    "note = E AND OLD.x = 1",
		wantWord:  "OLD",
		wantStart: 13,
	},

	// One string constant may be spelled as several. PostgreSQL joins two constants separated by
	// whitespace holding at least one newline into one and keeps the *first* spelling's escaping
	// rule across the join (scan.l's quotecontinue). The first row is the whole reason the rule is
	// modelled; the four after it hold its edges from the other side, because a join claimed for a
	// gap that does not join would hide a real reference instead
	// (.claude/rules/test-both-sides-of-an-exclusion-guard.md). Every one was measured on
	// PostgreSQL 18.4 and the measurements are quoted at continuationQuote.
	{
		// `SELECT quote_literal(E'a'<LF>'\' OLD ')` is the single string `a' OLD `, and
		// `WHEN (NEW.label = E'a'<LF>'\' OLD ')` is accepted on an INSERT trigger. A scanner that
		// ends the constant at the quote at 10 reopens the remainder under plain quoting, where
		// the backslash at 13 is inert, closes again at 14 and reports the OLD at 16 -- a false
		// positive on a clause the server accepts.
		//
		// n0 o1 t2 e3 sp4 =5 sp6 E7 '8 a9 '10 \n11 '12 \13 '14 sp15 O16 L17 D18 sp19 '20.
		name:      "a constant continued across a newline keeps the first spelling's escaping rule",
		operation: "insert",
		clause:    "note = E'a'\n" + `'\' OLD '`,
	},
	{
		// The other side of that rule: what the join carries is the first spelling's rule, so a
		// plain half's backslash stays inert -- `SELECT quote_literal('a'<LF>'\')` is the
		// two-character string `a\`. This constant therefore does end at 13, and the OLD after it
		// is code. A continuation that switched to escaping would run the constant to the quote
		// before `x` and miss the reference.
		//
		// n0 o1 t2 e3 sp4 =5 sp6 '7 a8 '9 \n10 '11 \12 '13 sp14 O15 R16 sp17 -- OLD begins at 18.
		name:      "a continued constant keeps plain quoting rather than gaining escapes",
		operation: "insert",
		clause:    "note = 'a'\n" + `'\' OR OLD.status = 'x'`,
		wantWord:  "OLD",
		wantStart: 18,
	},
	{
		// scan.l admits a `--` comment in the gap as well as whitespace, and the server agrees:
		// with the comment below the value is still the single string `a' OLD `. A gap rule of
		// whitespace alone ends the constant at 10 and reports the OLD at 21.
		name:      "a line comment may stand in the gap between two spellings of one constant",
		operation: "insert",
		clause:    "note = E'a' -- c\n" + `'\' OLD '`,
	},
	{
		// A `/* */` comment may not: the lookahead state has no rule for one, so the constant
		// ends there and `SELECT E'a' /* c */<LF>'\' OLD '` is a syntax error. The `'\'` after the
		// comment is then a plain literal holding one backslash, and the OLD after that is code.
		//
		// n0..sp6 E7 '8 a9 '10 sp11 /12 *13 sp14 c15 sp16 *17 /18 \n19 '20 \21 '22 sp23 -- so OLD
		// begins at 24.
		name:      "a block comment does not join two constants",
		operation: "insert",
		clause:    "note = E'a' /* c */\n" + `'\' OLD '`,
		wantWord:  "OLD",
		wantStart: 24,
	},
	{
		// And the newline is required rather than merely permitted: `SELECT E'a' '\' OLD '` is a
		// syntax error, so those are two constants and the OLD between them is code.
		//
		// n0..sp6 E7 '8 a9 '10 sp11 '12 \13 '14 sp15 -- so OLD begins at 16.
		name:      "horizontal whitespace alone does not join two constants",
		operation: "insert",
		clause:    "note = E'a' " + `'\' OLD '`,
		wantWord:  "OLD",
		wantStart: 16,
	},
	{
		// -0 -1 sp2 n3 o4 sp5 r6 e7 f8 e9 r10 e11 n12 c13 e14 sp15 h16 e17 r18 e19 \n20 --
		// so OLD begins at byte 21. A line comment that ran to the end of the input
		// instead of to its newline would find nothing here.
		name:      "a line comment ends at its newline",
		operation: "insert",
		clause:    "-- no reference here\nOLD.status = 'x'",
		wantWord:  "OLD",
		wantStart: 21,
	},
	{
		// Either newline byte ends one: scan.l spells the comment ("--"{non_newline}*) with
		// non_newline [^\n\r], so a carriage return ends this comment and the reference after it
		// is code. Measured: `SELECT 1 --c<CR>+2` is 3, while the same text without the CR is 1.
		//
		// -0 -1 sp2 c3 \r4 -- so OLD begins at byte 5.
		name:      "a line comment ends at a carriage return too",
		operation: "insert",
		clause:    "-- c\rOLD.status = 'x'",
		wantWord:  "OLD",
		wantStart: 5,
	},

	// Unterminated constructs. Each must end the scan without a panic and without a
	// finding: the rest of the clause is inside the construct, so none of it is code.
	{
		name:      "unterminated single quote",
		operation: "insert",
		clause:    "status = 'OLD",
	},
	{
		name:      "unterminated double quote",
		operation: "insert",
		clause:    `status = "OLD`,
	},
	{
		name:      "unterminated escape-string literal",
		operation: "insert",
		clause:    "status = E'OLD",
	},
	{
		name:      "unterminated dollar quote",
		operation: "insert",
		clause:    "status = $t$ OLD",
	},
	{
		name:      "unterminated untagged dollar quote",
		operation: "insert",
		clause:    "status = $$ OLD",
	},
	{
		name:      "unterminated block comment",
		operation: "insert",
		clause:    "status = 1 /* OLD",
	},
	{
		name:      "a line comment with no newline",
		operation: "insert",
		clause:    "-- OLD",
	},
	{
		name:      "empty clause",
		operation: "insert",
		clause:    "",
	},
	{
		name:      "a lone dollar sign",
		operation: "insert",
		clause:    "amount > $",
	},
	{
		// A pair of dollar signs around a tag that is not an identifier opens nothing, so
		// the reference after it is still reached. `$1$` rather than `$1` is what makes
		// this row test the tag rule: with only one dollar sign there is no closing
		// candidate to find, so the tag is never examined and the row would pass against
		// a scanner that accepted any tag at all
		// (.claude/rules/partition-named-test-cases.md -- a surviving mutant found this).
		//
		// a0 m1 o2 u3 n4 t5 sp6 >7 sp8 $9 110 $11 sp12 A13 N14 D15 sp16 -- so OLD begins
		// at byte 17.
		name:      "a dollar-quote tag beginning with a digit opens nothing",
		operation: "insert",
		clause:    "amount > $1$ AND OLD.total > 0",
		wantWord:  "OLD",
		wantStart: 17,
	},
	{
		// The other path into the same answer: a lone `$1` parameter placeholder has no
		// second dollar sign at all, so there is no delimiter to weigh.
		name:      "a parameter placeholder with no closing dollar sign",
		operation: "insert",
		clause:    "amount > $1 AND OLD.total > 0",
		wantWord:  "OLD",
		// a0 m1 o2 u3 n4 t5 sp6 >7 sp8 $9 110 sp11 A12 N13 D14 sp15 -- so OLD begins at 16.
		wantStart: 16,
	},

	// The accepting side of that guard. The `$1$` row proves the tag rule refuses what
	// PostgreSQL refuses; on its own it says nothing about whether the rule refuses too
	// widely, which is the failure these four rows exist to catch
	// (.claude/rules/test-both-sides-of-an-exclusion-guard.md). Each is a tag the server's
	// dolq_start/dolq_cont classes admit and an object-name rule would not.
	{
		name:      "a 63-byte dollar-quote tag opens a literal",
		operation: "insert",
		clause:    dollarQuotedClause(longTag),
	},
	{
		// One byte past NAMEDATALEN - 1. That bound governs names the catalog stores, and a
		// tag never becomes one, so this is a literal too -- the case that would be refused
		// by delegating the tag rule to a predicate carrying an object name's length bound.
		name:      "a dollar-quote tag one byte over the identifier limit opens a literal",
		operation: "insert",
		clause:    dollarQuotedClause(overlongTag),
	},
	{
		// \xc2\xb7 is U+00B7 MIDDLE DOT: two bytes above ASCII decoding to a symbol rather
		// than a letter. dolq_start admits every byte from \200 up whatever it decodes to, so
		// this is a tag even though the same text could not open a table name.
		name:      "a dollar-quote tag of high bytes that are not letters opens a literal",
		operation: "insert",
		clause:    dollarQuotedClause("\xc2\xb7"),
	},
	{
		// \xff decodes to no character at all. The server's tag rule is byte-wise, so a tag
		// need not be valid UTF-8 -- which is the half of the rule a rune-wise charset cannot
		// express however wide its letter classes are.
		name:      "a dollar-quote tag that is not valid UTF-8 opens a literal",
		operation: "insert",
		clause:    dollarQuotedClause("\xff"),
	},
	{
		// The ASCII classes the two rows above cannot reach, each in a position that
		// distinguishes it: `_` opens the tag, which only dolq_start admits; `1` continues it,
		// which only dolq_cont admits; and `T` is the upper-case half of the letter class,
		// which every other tag in this file happens to miss. Dropping any one of the three
		// makes the scanner refuse this delimiter and read the OLD inside it as code.
		name:      "a dollar-quote tag spanning every ASCII class the rule admits",
		operation: "insert",
		clause:    dollarQuotedClause("_tT1"),
	},
}

// TestForbiddenWhenReference is the pre-check R30 rests on: it must fire exactly when
// PostgreSQL would refuse the WHEN condition, and stay silent otherwise. Both halves are
// asserted from the one table above.
func TestForbiddenWhenReference(t *testing.T) {
	for _, tc := range whenClauseCases {
		t.Run(tc.name, func(t *testing.T) {
			ref, found := forbiddenWhenReference(tc.operation, tc.clause)

			if wanted := tc.wantWord != ""; found != wanted {
				t.Fatalf("forbiddenWhenReference(%q, %q) found = %v (%+v), want %v",
					tc.operation, tc.clause, found, ref, wanted)
			}
			if !found {
				return
			}

			if ref.Word != tc.wantWord {
				t.Errorf("reported word = %q, want %q", ref.Word, tc.wantWord)
			}
			if ref.Start != tc.wantStart {
				t.Errorf("reported start = %d, want %d", ref.Start, tc.wantStart)
			}
		})
	}
}

// TestADetectedReferenceReportsWhereItStarts covers CK-B9: R30's diagnostic has to land on
// the `when` line, and a `when` value's own node anchors on the block-scalar indicator
// rather than on its content, so the rule layer needs an index inside the clause.
//
// The assertion reads the clause at the reported index instead of comparing against a
// literal, so it states what the index *means* rather than restating the table above.
func TestADetectedReferenceReportsWhereItStarts(t *testing.T) {
	const clause = "t.x = 1 AND old.status <> 'paid'"

	ref, found := forbiddenWhenReference("insert", clause)

	if !found {
		t.Fatalf("forbiddenWhenReference(%q) found nothing", clause)
	}
	if got := clause[ref.Start : ref.Start+len(ref.Word)]; !strings.EqualFold(got, ref.Word) {
		t.Errorf("reported %q at byte %d, but the clause holds %q there", ref.Word, ref.Start, got)
	}
}

// TestAFindingWordsTheServerSentence asserts the finding carries enough to state which rule
// PostgreSQL would apply -- the row variable *and* the statement refusing it -- so Step 9 words
// R30's message from the finding instead of mapping the variable back to the operation and
// keeping a second copy of R30 in the rule layer.
//
// The sentence is composed here rather than in whenlex.go on purpose: the wording is the rule
// layer's (ADR-6), and what is asserted is that the fact suffices to produce it. Both inputs
// are lower case, so the row variable and the keyword are both the server's spelling and not
// the author's.
func TestAFindingWordsTheServerSentence(t *testing.T) {
	for _, tc := range []struct{ operation, clause, wantSentence string }{
		{
			operation:    "insert",
			clause:       "old.status = 'x'",
			wantSentence: "INSERT trigger's WHEN condition cannot reference OLD values",
		},
		{
			operation:    "delete",
			clause:       "new.status = 'x'",
			wantSentence: "DELETE trigger's WHEN condition cannot reference NEW values",
		},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			ref, found := forbiddenWhenReference(tc.operation, tc.clause)
			if !found {
				t.Fatalf("forbiddenWhenReference(%q, %q) found nothing", tc.operation, tc.clause)
			}

			sentence := ref.Statement + " trigger's WHEN condition cannot reference " +
				ref.Word + " values"
			if sentence != tc.wantSentence {
				t.Errorf("the finding words %q, want %q", sentence, tc.wantSentence)
			}
		})
	}
}

// whenRefusalByOperation restates R30 for every operation the configuration admits: the row
// variable PostgreSQL refuses a WHEN condition for, and the empty string where it refuses none.
// It is spelled out here rather than read from whenRefusal so the assertion states the rule
// instead of inheriting it.
var whenRefusalByOperation = map[string]string{
	"insert": "OLD",
	"update": "",
	"delete": "NEW",
}

// TestEveryConfiguredOperationHasAWhenRule pins the one vocabulary whenlex.go shares with
// another file: whenRefusal switches on the configuration's operation keys, and schema.go is
// where those keys are declared. Nothing else connects the two, and whenRefusal answers "nothing
// is forbidden" for a key it does not recognise -- so a key renamed on either side would leave
// R30 silently never firing, with every adversarial case still passing.
//
// The keys are read from the schema rather than listed again here, so a fourth operation added
// there without a rule stated for it fails too.
func TestEveryConfiguredOperationHasAWhenRule(t *testing.T) {
	keys := schemaLevels[levelOperations].keys

	if len(keys) != len(whenRefusalByOperation) {
		t.Fatalf("the configuration admits %d operations, but R30 is stated for %d",
			len(keys), len(whenRefusalByOperation))
	}

	for _, spec := range keys {
		t.Run(spec.name, func(t *testing.T) {
			want, stated := whenRefusalByOperation[spec.name]
			if !stated {
				t.Fatalf("the configuration admits operation %q, which R30 says nothing about",
					spec.name)
			}

			if got, _ := whenRefusal(spec.name); got != want {
				t.Errorf("whenRefusal(%q) forbids %q, want %q", spec.name, got, want)
			}
		})
	}
}

// nonCodeContexts are the constructs the scanner must not look inside -- one row per arm of
// nextToken's dispatch plus one *composition* of two arms, because a target is evidence only
// about the dimensions its generator varies
// (.claude/rules/generate-the-dimension-the-invariant-quantifies-over.md). Two dimensions were
// missing in turn, and each cost a false positive that millions of clean executions said nothing
// about: first the escape-string literal, the only construct with its own escaping rule, and then
// the composition of two constructs, which is where a *continued* string constant lives.
//
// open wraps the fuzzed text and close ends the construct. content spells that text as legal
// content of the construct, which for five of the seven means dropping the characters that would
// end it early: otherwise the text after an early end really is code, and a finding there would
// be right rather than a false positive.
//
// The escape-string rows are the ones whose content rule is not a subtraction. A doubled quote and
// `\'` are both a quote inside E'...', read under different rules, so escapeStringBody spells them
// alternately and one clause holds both -- the interaction no amount of dropping quotes can
// reach.
//
// The dollar-quoted row carries no open or close: fromTag asks for its delimiter to be built
// from the fuzzed tag instead, so exploration varies the tag rule rather than holding it at one
// spelling. The continued row is the composed one: continuationGap turns the fuzzed gap into
// whitespace that joins two constants, so its opener is a whole constant, a generated gap and the
// quote that reopens it -- after which the escaping rule of the first spelling still governs, which
// is why its content is spelled and not stripped.
var nonCodeContexts = []struct {
	name      string
	open      string
	close     string
	content   func(text string) string
	fromTag   bool
	continued bool
}{
	{name: "single-quoted literal", open: "'", close: "'", content: without("'")},
	{name: "double-quoted identifier", open: `"`, close: `"`, content: without(`"`)},
	{name: "line comment", open: "-- ", close: "\n", content: without("\n\r")},
	{name: "block comment", open: "/* ", close: " */", content: without("*/")},
	{name: "dollar-quoted literal", content: without("$"), fromTag: true},
	{name: "escape-string literal", open: "E'", close: "'", content: escapeStringBody},
	{
		name: "string constant continued across a newline", open: "E'a'", close: "'",
		content: escapeStringBody, continued: true,
	},
}

// forbiddenPairs are the two operation/row-variable pairs R30 refuses, spelled out here rather
// than read from forbiddenWhenReference's own mapping so that the target states the rule
// instead of inheriting it.
//
// `update` is absent deliberately. No row variable is forbidden for it, so an iteration over
// it could not produce a finding whatever the scanner did, and a loop that reads as
// three-operation coverage while delivering one is the blind spot
// .claude/rules/partition-named-test-cases.md names. That the scanner never fires for `update`
// is asserted where it can fail, in FuzzWhenScannerReportsARealWord.
var forbiddenPairs = []struct {
	operation string
	word      string
}{
	{operation: "insert", word: "OLD"},
	{operation: "delete", word: "NEW"},
}

// FuzzWhenScannerFindsNothingInsideNonCode generalises AC #18 over an unbounded input
// domain: for arbitrary text placed inside any of the seven constructs, the row variable
// forbidden for the operation is never a finding.
//
// Each pairing plants the word that operation actually forbids, so both iterations can fail.
// The construct is left unterminated when closed is false, which is the other half of the
// property. Everything after an unterminated opener is inside the construct, so no finding
// may come from there either -- and because the opener comes first, the fuzzed text cannot
// contribute a legitimate finding of its own.
//
// Exploration runs only under -fuzz; the committed seeds keep the default `go test`
// deterministic. The second seed's text carries two quotes, so the escape-string rows spell one
// of each and the doubled-quote interaction is covered by the default run rather than waiting
// for exploration to rediscover it. That same seed is what makes the continued row fail against a
// scanner that does not model the join, since the `\'` it spells only stays inert while the first
// spelling's escaping rule governs the second.
func FuzzWhenScannerFindsNothingInsideNonCode(f *testing.F) {
	for kind, context := range nonCodeContexts {
		f.Add(uint8(kind), true, "seed", "\n", "")
		f.Add(uint8(kind), false, "seed", "\n", "status = 'paid' ")

		if context.fromTag {
			// The four tag boundaries a fixed tag hid. Seeds run in the default `go test`, so
			// these pin the tag rule deterministically rather than waiting for exploration to
			// rediscover them.
			f.Add(uint8(kind), true, longTag, "\n", "")
			f.Add(uint8(kind), true, overlongTag, "\n", "")
			f.Add(uint8(kind), true, "\xc2\xb7", "\n", "")
			f.Add(uint8(kind), true, "\xff", "\n", "")
		}
		if context.continued {
			// Two gaps the hand-written rows do not spell: every other byte of scan.l's space
			// class, and one the generator has to supply a newline for itself.
			f.Add(uint8(kind), true, "seed", " \t\f\v\r ", `a'b'c`)
			f.Add(uint8(kind), true, "seed", "", `x\y'z'`)
		}
	}

	f.Fuzz(func(t *testing.T, kind uint8, closed bool, tag, gap, text string) {
		for _, pair := range forbiddenPairs {
			clause := nonCodeClause(kind, closed, tag, gap, text, pair.word)

			if ref, found := forbiddenWhenReference(pair.operation, clause); found {
				t.Fatalf("forbiddenWhenReference(%q, %q) reported %q at %d; every occurrence is inside a %s",
					pair.operation, clause, ref.Word, ref.Start,
					nonCodeContexts[int(kind)%len(nonCodeContexts)].name)
			}
		}
	})
}

// FuzzWhenScannerReportsARealWord asserts the two invariants that hold for every input,
// including the malformed: the scan terminates without panicking, and a reported index
// really points at the word that was reported. An index that drifted would send R30's
// caret to the wrong place, or out of bounds.
//
// Its seed corpus is the whole case table, so exploration starts from AC #18's six
// adversarial cases and from the unterminated constructs.
func FuzzWhenScannerReportsARealWord(f *testing.F) {
	for _, seed := range whenClauseCases {
		f.Add(seed.clause)
	}

	f.Fuzz(func(t *testing.T, clause string) {
		for _, operation := range []string{"insert", "update", "delete"} {
			ref, found := forbiddenWhenReference(operation, clause)
			if !found {
				continue
			}

			if operation == "update" {
				t.Fatalf("an UPDATE WHEN condition may reference both row variables, yet %q was reported", ref.Word)
			}
			if ref.Start < 0 || ref.Start+len(ref.Word) > len(clause) {
				t.Fatalf("reported %q at %d, which is outside a clause of %d bytes",
					ref.Word, ref.Start, len(clause))
			}
			if got := clause[ref.Start : ref.Start+len(ref.Word)]; !strings.EqualFold(got, ref.Word) {
				t.Fatalf("reported %q at %d, but the clause holds %q there", ref.Word, ref.Start, got)
			}
		}
	})
}

// nonCodeClause places word inside one of the seven constructs, having first spelled text as
// content that construct cannot be ended by.
//
// The dollar-quoted construct builds its delimiter from the fuzzed tag, coerced by legalTag so
// that the clause really is a literal by PostgreSQL's rule whatever bytes the fuzzer supplied. The
// continued construct builds the rest of its opener from the fuzzed gap the same way.
func nonCodeClause(kind uint8, closed bool, tag, gap, text, word string) string {
	context := nonCodeContexts[int(kind)%len(nonCodeContexts)]

	opener, closer := context.open, context.close
	if context.fromTag {
		delimiter := "$" + legalTag(tag) + "$"
		opener, closer = delimiter, delimiter
	}
	if context.continued {
		opener += continuationGap(gap) + "'"
	}

	clause := opener + context.content(text) + " " + word + " "
	if closed {
		clause += closer
	}
	return clause
}

// The gap classes PostgreSQL joins two string constants across, spelled out here from
// src/backend/parser/scan.l rather than read from whenlex.go so that the generator and the scanner
// are independent: space [ \t\n\r\f\v], and the newline [\n\r] within it. A scanner whose gap rule
// narrowed would then refuse a gap produced here, read the second spelling as a constant of its
// own, and fail the target -- which sharing its constants would hide.
const (
	gapSpaceBytes   = " \t\n\r\f\v"
	gapNewlineBytes = "\n\r"
)

// continuationGap coerces arbitrary bytes into a gap that really does join two string constants:
// only the space class survives, and a newline is added when the fuzzer supplied none.
//
// Both halves are load-bearing, and in the same direction. A gap holding anything else, or holding
// no newline, leaves the second spelling a separate constant -- and then the `\'` that
// escapeStringBody writes into it really does end that constant, so the word after it really is
// code and a finding there would be right rather than a false positive.
func continuationGap(gap string) string {
	spaces := strings.Map(func(char rune) rune {
		if !strings.ContainsRune(gapSpaceBytes, char) {
			return -1
		}
		return char
	}, gap)

	if strings.ContainsAny(spaces, gapNewlineBytes) {
		return spaces
	}
	return spaces + "\n"
}

// without returns a content rule that drops every character of unwanted, which is how five of
// the six constructs keep the fuzzed text from ending them early.
func without(unwanted string) func(text string) string {
	return func(text string) string {
		return strings.Map(func(char rune) rune {
			if strings.ContainsRune(unwanted, char) {
				return -1
			}
			return char
		}, text)
	}
}

// escapeStringBody spells text as content of an E'...' literal.
//
// PostgreSQL accepts a quote inside one written either way -- doubled, which is scan.l's xqdouble
// production and runs in the escape-string state as well as the plain one, or `\'` -- so the two
// spellings alternate and one clause holds both. Neither can end the literal, and a backslash is
// always doubled, so not even a trailing one escapes the closing quote.
//
// Dropping quotes the way the other five rows do would have removed the very sequence that
// distinguishes the two escaping rules, which is why this row spells its content instead. It
// works on bytes rather than runes so the fuzzer's invalid UTF-8 reaches the scanner intact --
// the alphabet a byte-wise lexer has to be explored with.
func escapeStringBody(text string) string {
	var body strings.Builder

	doubleTheQuote := true
	for index := range len(text) {
		switch char := text[index]; char {
		case '\'':
			quote := `\'`
			if doubleTheQuote {
				quote = "''"
			}
			body.WriteString(quote)
			doubleTheQuote = !doubleTheQuote
		case '\\':
			body.WriteString(`\\`)
		default:
			body.WriteByte(char)
		}
	}
	return body.String()
}

// legalTag coerces arbitrary bytes into a dollar-quote tag PostgreSQL's lexer accepts, keeping
// the bytes its two tag classes admit and dropping the rest. An empty result is returned as it
// is: `$$` is the untagged form, equally a construct the scanner must not look inside.
//
// The classes are spelled out below from the grammar rather than read from whenlex.go's own
// predicate, so the coercion and the scanner are independent. A scanner whose tag rule is
// narrower than the server's then refuses a tag produced here, reads the literal's body as
// code, finds the row variable in it and fails the target. Sharing the scanner's predicate
// would have made the coercion agree with any narrowing in it and hidden exactly that.
func legalTag(tag string) string {
	var legal strings.Builder

	for index := range len(tag) {
		char := tag[index]

		// dolq_cont admits the ten digits and dolq_start does not, so a leading digit is
		// dropped rather than allowed to open the tag.
		legalHere := isDolqContByte(char)
		if legal.Len() == 0 {
			legalHere = isDolqStartByte(char)
		}

		if legalHere {
			legal.WriteByte(char)
		}
	}
	return legal.String()
}

// isDolqStartByte reports whether a byte may open a dollar-quote tag, as PostgreSQL 17's
// src/backend/parser/scan.l spells the class: dolq_start [A-Za-z\200-\377_].
func isDolqStartByte(char byte) bool {
	isASCIILetter := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
	return isASCIILetter || char == '_' || char >= 0x80
}

// isDolqContByte reports whether a byte may continue one: dolq_cont [A-Za-z\200-\377_0-9].
func isDolqContByte(char byte) bool {
	return isDolqStartByte(char) || char >= '0' && char <= '9'
}

// commentsOf returns the text of every comment in one file of this package.
//
// It parses the file itself rather than using packageSyntax, which discards comments on
// purpose: a prohibition that could be satisfied by editing a comment would assert what a
// file says about itself. This assertion is the opposite kind -- the note it looks for is a
// deliverable -- so it is the one place that has to read them.
func commentsOf(t *testing.T, name string) string {
	t.Helper()

	file, err := goparser.ParseFile(gotoken.NewFileSet(), name, nil,
		goparser.ParseComments|goparser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s failed: %v", name, err)
	}

	var text strings.Builder
	for _, group := range file.Comments {
		text.WriteString(group.Text())
	}
	return text.String()
}

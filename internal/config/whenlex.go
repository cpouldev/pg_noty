package config

import "strings"

// TRUST BOUNDARY -- read this before extending anything in this file.
//
// A `when` value is raw SQL, and pg_noty puts it verbatim into a trigger's WHEN clause.
// Anyone who can edit the configuration file can therefore execute arbitrary SQL inside the
// transaction that writes to the target table, running as the trigger function's owner.
// Environment interpolation is permitted in a `when` value, which extends that same reach to
// whoever sets the variable. The risk is the database owner's to carry: those are their
// production tables the statement runs against, and enqueue fails closed, so SQL that errors
// stops their application's writes.
//
// Nothing here narrows that boundary, and nothing can -- the field is necessarily raw SQL.
// What this file does is much smaller, and worth stating exactly so that no reader mistakes
// it for a sandbox: it is a *lexical* pre-check for the one class of mistake PostgreSQL
// refuses at DDL time, a WHEN condition naming the row variable its operation does not have.
// Catching it here turns a failed migration into a line number. Parsing the SQL for real is
// phase 4's WhenParses deferred check, against a live catalog. The same boundary is stated
// for consumers of the resolved configuration on Operation.When in config.go.

// The vocabulary R30 is stated in.
const (
	// The two row variables a trigger WHEN condition can be refused for referencing, and the
	// statement keywords PostgreSQL names in those two refusals -- both spelled as PostgreSQL
	// spells them in its own error messages rather than as the configuration spells its keys.
	oldRowVariable  = "OLD"
	newRowVariable  = "NEW"
	insertStatement = "INSERT"
	deleteStatement = "DELETE"

	// The configuration's own spelling of the two operations something is forbidden for: the
	// keys written under `operations`, in the contract's lower case.
	//
	// They are the one vocabulary this file shares with another -- schema.go declares the same
	// three keys -- and whenRefusal answers "nothing is forbidden" for a key it does not
	// recognise, so a spelling that drifted apart would leave R30 silently never firing while
	// every adversarial case still passed. TestEveryConfiguredOperationHasAWhenRule reads the
	// keys from the schema and fails on that drift. Step 8 owns folding the three into one
	// declaration, which config.go assigns to it as the first code that compares against them.
	insertOperationKey = "insert"
	deleteOperationKey = "delete"
)

// The text and the bytes PostgreSQL's lexer opens each construct with.
const (
	lineCommentOpen   = "--"
	blockCommentOpen  = "/*"
	blockCommentClose = "*/"

	// escapeStringPrefix opens a literal whose backslashes escape: E'a\'b'.
	escapeStringPrefix = "E"

	// stringQuote delimits a string constant and identifierQuote a quoted identifier. Only the
	// first opens a construct that several spellings can make one of -- see continuationQuote.
	stringQuote     = '\''
	identifierQuote = '"'

	// fieldSelector separates a row from one of its fields: in `NEW.old` it is what makes `old`
	// the name of a column rather than a reference to the OLD row.
	fieldSelector = '.'

	// spaceBytes is scan.l's space class [ \t\n\r\f\v] and newlineBytes the newline [\n\r]
	// within it, transcribed for the reason the word classes at the foot of this file are: they
	// are the server's classes, and its lexer reads them byte-wise.
	spaceBytes   = " \t\n\r\f\v"
	newlineBytes = "\n\r"
)

// firstByteAboveASCII is where PostgreSQL's word and dollar-quote tag classes begin admitting
// bytes unconditionally -- scan.l spells that half of each class \200-\377.
const firstByteAboveASCII = 0x80

// The two quoting rules endOfQuoted is asked for, named so that a call site says which it
// means rather than passing a bare boolean.
const (
	honourBackslashEscapes = true
	plainQuoting           = false
)

// bareReference is a row-variable reference found in code -- outside every literal, quoted
// identifier and comment -- in a WHEN condition.
type bareReference struct {
	// Word is the row variable as PostgreSQL names it in its refusal, in that spelling
	// whatever the author wrote.
	Word string
	// Statement is the statement keyword PostgreSQL names in that same refusal, INSERT or
	// DELETE.
	//
	// It is carried beside Word because the server words its sentence from both -- "INSERT
	// trigger's WHEN condition cannot reference OLD values" -- so a caller composes that
	// sentence from the finding rather than mapping the row variable back to the half of R30
	// it came from. Keeping that mapping in one place is the point: it already exists in
	// whenRefusal, and a rule layer re-deriving it would be a second copy of the rule.
	//
	// It is the server's keyword and not the operation as the author wrote it, which the
	// caller passed in and therefore already has.
	Statement string
	// Start is the byte index of the reference within the clause.
	//
	// A `when` value's own node anchors on its block-scalar indicator rather than on its
	// content (Implementation Note 8), so a caller pointing inside the body adds this to
	// that anchor. It is an index and not a column: deriving a column is position.go's
	// alone (ADR-4).
	Start int
}

// forbiddenWhenReference reports the first row-variable reference a PostgreSQL trigger WHEN
// condition may not make for this operation. It is the pre-check behind R30.
//
// Verified on PostgreSQL 17.10: an INSERT trigger's WHEN condition cannot reference OLD
// ("INSERT trigger's WHEN condition cannot reference OLD values"), and a DELETE trigger's
// cannot reference NEW ("DELETE trigger's WHEN condition cannot reference NEW values"). It
// is the same measurement that forces one trigger per operation rather than one per
// listener, so this rule and that design decision have one source.
//
// The result is a fact and not a diagnostic: which RuleID it belongs to, how it is worded
// and which token it anchors on are the rule layer's (ADR-6).
func forbiddenWhenReference(operation, clause string) (bareReference, bool) {
	variable, statement := whenRefusal(operation)
	if variable == "" {
		return bareReference{}, false
	}

	start, found := firstBareWord(clause, variable)
	if !found {
		return bareReference{}, false
	}
	return bareReference{Word: variable, Statement: statement, Start: start}, true
}

// whenRefusal names how PostgreSQL refuses a WHEN condition for the given operation: the row
// variable it may not reference, and the statement keyword the refusal is worded with. Both
// are empty when the operation is refused nothing.
//
// An UPDATE trigger has both rows, so nothing is forbidden for it. Any other operation is
// R28's to reject, so nothing is forbidden for it either: guessing here would report a rule
// about a statement that does not exist.
//
// The pairing is R30's whole content and lives only here, which is why both halves are
// returned together rather than a caller mapping one to the other.
func whenRefusal(operation string) (variable, statement string) {
	switch operation {
	case insertOperationKey:
		return oldRowVariable, insertStatement
	case deleteOperationKey:
		return newRowVariable, deleteStatement
	default:
		return "", ""
	}
}

// firstBareWord reports where the first bare occurrence of word begins in clause: an occurrence
// that is a whole word, that lies outside every construct nextToken skips -- string constant,
// quoted identifier, line comment, block comment, dollar-quoted literal -- and that is not the
// field name after a selector, where the same spelling names a column of the row rather than the
// row itself (.claude/rules/same-spelling-is-not-the-same-construct.md).
//
// That is the whole of the claim, and stating it no wider is deliberate: a construct nextToken
// does not model would leave its body to be read as code, and a position where the grammar reads
// this spelling as something else would be reported.
//
// found is false when there is none, which includes the case of an unterminated construct:
// the rest of the clause is inside it, so none of that is code. The zero returned alongside
// is not a sentinel -- byte 0 is a legal answer -- so the boolean is what a caller has to
// read (.claude/rules/scan-result-sentinels.md).
//
// The scan consumes whole tokens rather than searching for the word, which is what makes
// both word boundaries hold with no look-behind: `old_price`, `old$price`, `newest` and
// `xOLD` are each a single word, and none of them is OLD or NEW.
func firstBareWord(clause, word string) (int, bool) {
	afterFieldSelector := false

	for index := 0; index < len(clause); {
		end, isWord := nextToken(clause, index)

		if isWord && !afterFieldSelector && strings.EqualFold(clause[index:end], word) {
			return index, true
		}
		if !isTokenSeparator(clause, index) {
			afterFieldSelector = clause[index] == fieldSelector
		}
		index = end
	}
	return 0, false
}

// isTokenSeparator reports whether the construct at index separates two tokens without being one
// itself, leaving the token before it standing as the one a following word is read against.
//
// It is why a selector reaches the name it selects across whitespace and comments: PostgreSQL 18.4
// accepts `WHEN (NEW . old > 1)`, `WHEN (NEW./* pick */old > 1)` and `WHEN (NEW.-- pick<LF>qty >
// 1)` on an INSERT trigger, each of them selecting a field.
func isTokenSeparator(clause string, index int) bool {
	return strings.IndexByte(spaceBytes, clause[index]) >= 0 ||
		strings.HasPrefix(clause[index:], lineCommentOpen) ||
		strings.HasPrefix(clause[index:], blockCommentOpen)
}

// nextToken reports where the construct beginning at index ends, and whether that construct
// is a bare word -- the only kind the caller looks at. Everything else is a literal, a
// quoted identifier, a comment or a single character of punctuation, and is skipped whole.
//
// index must lie within clause. Every branch answers with an index strictly greater than the
// one it was given, which is what makes the scan terminate on any input at all, unterminated
// constructs included.
//
// A doubled quote is not a case here: it is content of a construct rather than a construct of its
// own, so endOfQuoted consumes the pair. Dispatching it instead -- closing the literal at the
// first quote and opening another at the second -- is the reading endOfQuoted's comment records as
// correct under plain quoting and wrong under backslash escaping.
func nextToken(clause string, index int) (int, bool) {
	rest := clause[index:]

	switch {
	case isWordStartByte(rest[0]):
		return endOfWordOrEscapeString(clause, index)
	case strings.HasPrefix(rest, lineCommentOpen):
		return endOfLineComment(clause, index), false
	case strings.HasPrefix(rest, blockCommentOpen):
		return endOfBlockComment(clause, index), false
	case rest[0] == stringQuote, rest[0] == identifierQuote:
		return endOfQuoted(clause, index, rest[0], plainQuoting), false
	case rest[0] == '$':
		return endOfDollarQuoted(clause, index), false
	default:
		return index + 1, false
	}
}

// endOfWordOrEscapeString consumes the word at index -- and with it the literal that word
// opens, when the word is the E prefix.
//
// E'...' is one token, and its backslashes escape, which the backslashes of a plain '...'
// do not: standard_conforming_strings has been on by default since PostgreSQL 9.1. Reading
// either as the other moves the literal's end, and whatever lies past that end reads as code.
//
// The word's own first byte is consumed without being tested, which is what makes the answer always
// greater than index. Testing it would return index for a byte that cannot begin a word, and a
// caller advancing by that would not advance at all -- so the termination this function owes its
// caller is structural here rather than a precondition the caller has to keep.
func endOfWordOrEscapeString(clause string, index int) (int, bool) {
	end := endOfByteRun(clause, index+1, isWordByte)

	quoteFollows := end < len(clause) && clause[end] == stringQuote
	if quoteFollows && strings.EqualFold(clause[index:end], escapeStringPrefix) {
		return endOfQuoted(clause, end, stringQuote, honourBackslashEscapes), false
	}
	return end, true
}

// endOfByteRun reports where the run of bytes admits accepts, beginning at start, ends: the first
// index at or after start that admits refuses, or len(text) when it refuses none.
//
// The two byte-class walks this file needs are one shape -- how far a word extends, and whether a
// dollar-quote tag is nothing but tag bytes -- so the walk lives here once and each caller names
// its own class from the productions transcribed at the foot of the file.
func endOfByteRun(text string, start int, admits func(byte) bool) int {
	for at := start; at < len(text); at++ {
		if !admits(text[at]) {
			return at
		}
	}
	return len(text)
}

// endOfQuoted reports where the literal or quoted identifier opened at index ends: one past
// its closing quote, or the end of the clause when it is never closed.
//
// A doubled quote is one quote of content and closes nothing, under every quoting rule this
// scanner applies: scan.l's xqdouble production runs in the escape-string state as well as the
// plain one, its xddouble twin does the same for a quoted identifier, and the manual (4.1.2.2)
// documents a doubled quote as well as `\'` inside an E'...'. So the pair is consumed whatever
// backslashEscapes says -- one branch, not two -- and handled here rather than argued away.
//
// Under plain quoting, and only there, a reading that closed at the first quote and reopened at
// the second would agree about which stretches are code, because a backslash is inert in both
// readings. Under backslash escaping it does not: the reopened half reads a later `\'` as its
// close, ends the literal early and hands the interior to the caller as code. The server reads
//
//	E'a''\' OLD '
//
// as one string constant, and this scanner reported the OLD inside it -- what an equivalence
// claimed for every input rather than for the branch it was proved on costs
// (.claude/rules/pin-a-claimed-equivalence.md). There is a case per quoting rule in the table.
//
// A closing quote is not always the end either, because one string constant may be spelled as
// several: continuationQuote holds that rule, and backslashEscapes carries into every spelling
// because carrying it is what the rule is for. Its answer lies past the quote it was asked about,
// so every branch of this loop still advances.
func endOfQuoted(clause string, index int, quote byte, backslashEscapes bool) int {
	for at := index + 1; at < len(clause); at++ {
		if backslashEscapes && clause[at] == '\\' {
			at++ // whatever follows a backslash cannot close the literal
			continue
		}
		if clause[at] != quote {
			continue
		}
		if at+1 < len(clause) && clause[at+1] == quote {
			at++ // neither quote of a doubled pair closes the literal
			continue
		}

		continuation, continues := continuationQuote(clause, at+1, quote)
		if !continues {
			return at + 1
		}
		at = continuation // this quote reopens the same constant, so it closes nothing either
	}
	return len(clause)
}

// continuationQuote reports where the string constant whose closing quote ends at after continues
// -- the index of the quote that reopens it -- and false when nothing continues it there.
//
// PostgreSQL reads two string constants separated by whitespace holding at least one newline as
// one constant, keeping the first spelling's escaping rule across the join.
// src/backend/parser/scan.l (REL_17_STABLE) spells the gap
//
//	space			[ \t\n\r\f\v]
//	non_newline_space	[ \t\f\v]
//	newline			[\n\r]
//	comment			("--"{non_newline}*)
//	special_whitespace	({space}+|{comment}{newline})
//	non_newline_whitespace	({non_newline_space}|{comment})
//	whitespace_with_newline	({non_newline_whitespace}*{newline}{special_whitespace}*)
//	quotecontinue		{whitespace_with_newline}{quote}
//
// -- so a `--` comment may stand in the gap as well as whitespace. Both places one may stand in
// require the newline after it that ends it, and endOfLineComment steps past that newline, so "a
// run of whitespace and `--` comments holding at least one newline" admits exactly those gaps and
// no others. A `/* */` comment is in neither class, and the lookahead state this implements has no
// rule for one, so it ends the constant instead.
//
// Verified on PostgreSQL 18.4: `SELECT quote_literal(E'a'<LF>'\' OLD ')` is the single string
// `a' OLD `, as is the same text with a `--` comment in the gap; a `/* */` comment there and a gap
// without a newline are each a syntax error; and `CREATE TRIGGER ... BEFORE INSERT ... WHEN
// (NEW.label = E'a'<LF>'\' OLD ')` is accepted. That last one is the false positive this rule
// exists to prevent -- without the join the second spelling reads as a plain literal, its `\'`
// closes it, and an OLD inside a string constant is reported as code
// (.claude/rules/match-every-spelling-the-grammar-permits.md).
func continuationQuote(clause string, after int, quote byte) (int, bool) {
	// The lookahead is entered from scan.l's string states only and never from the quoted
	// identifier's, so `SELECT 1 AS "a"<LF>"b"` is a syntax error rather than one identifier.
	//
	// No clause can tell this guard from its absence, and that is a measured fact rather than the
	// reason it is here: joining two quoted identifiers would resume the same body scan at the same
	// index under the same rule, since a quoted identifier has no escaping variant for the join to
	// carry -- carrying one is the whole observable content of the rule. The guard transcribes the
	// grammar instead of resting on that agreement, which is what keeps a later escaping rule for
	// some other quote from inheriting a join the server does not make.
	if quote != stringQuote {
		return 0, false
	}

	gap := strings.TrimLeft(clause[after:], spaceBytes)
	for strings.HasPrefix(gap, lineCommentOpen) {
		gap = strings.TrimLeft(gap[endOfLineComment(gap, 0):], spaceBytes)
	}

	// Three ways nothing is continued here: a gap that crosses no newline, a gap that runs to the
	// end of the clause, and one that ends at anything but another quote of the same kind.
	at := len(clause) - len(gap)
	if !strings.ContainsAny(clause[after:at], newlineBytes) || gap == "" || gap[0] != quote {
		return 0, false
	}
	return at, true
}

// endOfLineComment reports where the `--` comment opened at index ends: one past the newline that
// ends it, or the end of the clause when it has none.
//
// Either newline byte ends one -- scan.l spells the comment ("--"{non_newline}*) with non_newline
// [^\n\r] -- so a carriage return ends it as surely as a line feed does, and what follows that byte
// is code. Measured on PostgreSQL 18.4: `SELECT 1 --c<CR>+2` is 3, and 1 without the carriage
// return.
func endOfLineComment(clause string, index int) int {
	if length := strings.IndexAny(clause[index:], newlineBytes); length >= 0 {
		return index + length + 1
	}
	return len(clause)
}

// endOfBlockComment reports where the `/* */` comment opened at index ends.
//
// PostgreSQL nests block comments, unlike C, so this counts depth: `/* /* */ */` is one
// comment. Stopping at the first `*/` would end the comment early and read the remainder of
// it as code.
//
// Each token is stepped over whole rather than one byte at a time, and that is load-bearing
// rather than tidiness: `/*` and `*/` share the `*`, so advancing a single byte past an opener
// would offer that `*` to the closing test and read `/*/` -- unterminated for the server -- as
// a comment closed by its own opener.
func endOfBlockComment(clause string, index int) int {
	const commentTokenBytes = len(blockCommentOpen)

	for at, depth := index, 0; at < len(clause); {
		switch {
		case strings.HasPrefix(clause[at:], blockCommentOpen):
			depth, at = depth+1, at+commentTokenBytes
		case strings.HasPrefix(clause[at:], blockCommentClose):
			depth, at = depth-1, at+commentTokenBytes
			if depth == 0 {
				return at
			}
		default:
			at++
		}
	}
	return len(clause)
}

// endOfDollarQuoted reports where the dollar-quoted literal opened at index ends. When the
// text there opens no literal -- `$1`, or a lone `$` -- the single character is consumed
// instead, so a reference following it is still reached.
//
// The closing delimiter is the opening tag exactly: `$outer$ ... $inner$ ... $outer$` is one
// literal, so a scan ending at the next `$...$` would read the text after the inner tag as
// code. That is the subtle half of dollar-quoting, and the reason for matching tags rather
// than hunting dollar signs.
func endOfDollarQuoted(clause string, index int) int {
	delimiter, opens := dollarQuoteDelimiter(clause, index)
	if !opens {
		return index + 1
	}

	body := index + len(delimiter)
	if length := strings.Index(clause[body:], delimiter); length >= 0 {
		return body + length + len(delimiter)
	}
	return len(clause)
}

// dollarQuoteDelimiter returns the delimiter opening a dollar-quoted literal at index --
// `$$`, or `$tag$` -- and reports false when nothing is opened there. An empty tag is the
// `$$...$$` form.
func dollarQuoteDelimiter(clause string, index int) (string, bool) {
	rest := clause[index+1:]

	end := strings.IndexByte(rest, '$')
	if end < 0 {
		return "", false
	}
	if tag := rest[:end]; tag != "" && !isDollarQuoteTag(tag) {
		return "", false
	}
	return clause[index : index+end+2], true
}

// isDollarQuoteTag reports whether text may tag a dollar-quoted literal: one opening byte
// followed by tag-continuation bytes, per the dolqdelim production below. It is what keeps `$1$`
// from opening a literal, since a digit cannot begin a tag -- and, because that production carries
// no length check, what lets a 64-byte tag open one.
func isDollarQuoteTag(text string) bool {
	// dolq_start and ident_start are one class, which is why a word's opening predicate answers
	// for a tag's first byte too. text is never empty: its only caller tests that first.
	return isWordStartByte(text[0]) && endOfByteRun(text, 1, isTagByte) == len(text)
}

// The byte classes PostgreSQL builds a word and a dollar-quote tag from, read from its own lexer,
// src/backend/parser/scan.l (REL_17_STABLE), which spells them
//
//	ident_start	[A-Za-z\200-\377_]
//	ident_cont	[A-Za-z\200-\377_0-9\$]
//	identifier	{ident_start}{ident_cont}*
//	dolq_start	[A-Za-z\200-\377_]
//	dolq_cont	[A-Za-z\200-\377_0-9]
//	dolqdelim	\$({dolq_start}{dolq_cont}*)?\$
//
// -- so a word and a tag open on one class, a word continues on that class plus the digits and the
// dollar sign, a tag continues on the same without the dollar sign, and neither production carries
// a length check.
//
// They are transcribed here, byte for byte and with nothing borrowed, rather than delegated to
// ident.go's isIdentifierStart / isIdentifierChar / identifierDefect, which enforce sub-rules a
// lexer token does not share (.claude/rules/reuse-composite-predicates-per-sub-rule.md):
//
//   - Those predicates are rune-wise and narrow the bytes above ASCII to the ones that decode to
//     *letters*, knowingly and for a reason belonging to table names (ident.go:72-77). Both classes
//     above admit every byte from \200 up whatever it decodes to, so neither a word nor a tag need
//     be valid UTF-8, and both are read byte-wise here for that reason.
//   - identifierDefect's 63-byte bound is NAMEDATALEN, a limit on names the catalog stores. A lexer
//     token never becomes a name.
//
// Every one of those narrowings costs a false positive on a clause PostgreSQL accepts, which is
// the failure AC #18 exists to prevent: a refused delimiter leaves the literal's body to be
// scanned as code, and a word broken at a byte above ASCII exposes the OLD it abuts. PostgreSQL's
// documentation describes a tag as following "the same rules as an unquoted identifier, except
// that it cannot contain a dollar sign", which is what makes the delegation look exact; the lexer
// above is the rule.

// isWordStartByte reports whether a byte may open a word -- ident_start, the class dolq_start
// spells identically, so it opens a dollar-quote tag as well.
func isWordStartByte(char byte) bool {
	isASCIILetter := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
	return isASCIILetter || char == '_' || char >= firstByteAboveASCII
}

// isWordByte reports whether a byte may continue one -- ident_cont, dolq_cont plus the dollar
// sign. That sign is why `old$price` is one word rather than `old` followed by a dollar-quoted
// literal.
func isWordByte(char byte) bool {
	return isTagByte(char) || char == '$'
}

// isTagByte reports whether a byte may continue a dollar-quote tag -- dolq_cont, the opening class
// plus the ten digits and nothing more.
//
// It does not also admit the dollar sign a word continues on, and does not need to: the text
// weighed as a tag is bounded by the clause's next dollar sign, so one cannot occur inside it.
//
// The ten digits are spelled out rather than taken from ident.go's rune-wise isDigit, so that the
// only thing this class depends on is the production quoted above. Reaching across a file for half
// of it left a widening there free to widen the tag rule with nothing pinning the pair.
func isTagByte(char byte) bool {
	return isWordStartByte(char) || char >= '0' && char <= '9'
}

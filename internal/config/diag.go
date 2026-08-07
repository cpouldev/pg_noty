package config

// RuleID names the rule that produced a diagnostic. It exists so that fixture
// coverage can be inventoried mechanically: every diagnostic states which rule
// raised it, and the corpus is checked against the enumerated lists below.
//
// RuleID is deliberately absent from both the de-duplication key and the sort key,
// and is never rendered, so tagging a diagnostic can never perturb rendered output.
// Its one influence is to break the tie between duplicates that are otherwise
// identical, where either survivor renders the same text anyway (see prefersOver).
type RuleID string

// The 42 static rules of the configuration contract. Their full statements live in
// the task specification's rule table; the names here exist to tag diagnostics and
// fixtures so coverage is countable rather than asserted.
const (
	R1  RuleID = "R1"  // version is present and equals 1
	R2  RuleID = "R2"  // instance charset and length
	R3  RuleID = "R3"  // database.url is present and parses
	R4  RuleID = "R4"  // database.schema is a valid identifier and not pg_ prefixed
	R5  RuleID = "R5"  // database.listen_url parses when present
	R6  RuleID = "R6"  // worker.concurrency range
	R7  RuleID = "R7"  // worker.batch_size range
	R8  RuleID = "R8"  // worker durations are positive
	R9  RuleID = "R9"  // worker.lease_timeout exceeds the largest listener timeout
	R10 RuleID = "R10" // retention durations are positive
	R11 RuleID = "R11" // retention.keep covers at least one partition interval
	R12 RuleID = "R12" // retention.precreate covers at least one partition interval
	R13 RuleID = "R13" // timeout is a positive duration
	R14 RuleID = "R14" // retry.max_attempts is at least 1
	R15 RuleID = "R15" // retry.backoff enum
	R16 RuleID = "R16" // retry.initial_interval does not exceed max_interval
	R17 RuleID = "R17" // retry.max_interval is a positive duration
	R18 RuleID = "R18" // retry.jitter is a boolean
	R19 RuleID = "R19" // header names are valid HTTP field names with non-empty values
	R20 RuleID = "R20" // no two header names differ only in case
	R21 RuleID = "R21" // no header uses the reserved X-Pg-Noty- prefix
	R22 RuleID = "R22" // the listeners key is present; an empty list is valid
	R23 RuleID = "R23" // listener name is present and matches the name charset
	R24 RuleID = "R24" // listener names are unique
	R25 RuleID = "R25" // listener enabled is a boolean
	R26 RuleID = "R26" // listener table is present and in schema.table form
	R27 RuleID = "R27" // operations is present with at least one entry
	R28 RuleID = "R28" // operation keys are insert, update or delete without duplicates
	R29 RuleID = "R29" // per-operation columns are legal only under update
	R30 RuleID = "R30" // when references no OLD under insert and no NEW under delete
	R31 RuleID = "R31" // payload.mode enum
	R32 RuleID = "R32" // payload.columns is required exactly when mode is columns
	R33 RuleID = "R33" // payload.exclude is legal only with mode full
	R34 RuleID = "R34" // payload.include_old is a boolean
	R35 RuleID = "R35" // payload.max_bytes is greater than zero
	R36 RuleID = "R36" // destination.url is present, parses, and is http or https
	R37 RuleID = "R37" // destination.method enum
	R38 RuleID = "R38" // signing secrets are non-empty and unique
	R39 RuleID = "R39" // listener concurrency range and ceiling
	R40 RuleID = "R40" // durations parse with Go units only
	R41 RuleID = "R41" // no unknown keys at any nesting level
	R42 RuleID = "R42" // no duplicate mapping keys
)

// The two non-fatal warnings of the configuration contract.
const (
	W1 RuleID = "W1" // a signing secret is a YAML literal rather than an env reference
	W2 RuleID = "W2" // drain_timeout is smaller than the largest listener timeout
)

// The pipeline's structural stages report conditions that are not numbered rules:
// a file that cannot be read, a document that does not parse, a document whose
// shape rules out every later stage, a reference to the environment that cannot be
// substituted, an alias or merge key that cannot be reduced to the value it names,
// a value whose YAML shape is not the shape the contract declares for its key, and
// a value whose text is not readable as the type the contract declares.
// They are named so that a caller can tell a structural failure from a rule
// violation, and they are outside the numbered inventory that fixture coverage is
// counted against.
//
// RuleDecode is the seventh, and it is the one the contract *almost* numbers: the
// rule table states a type for many keys -- an integer for worker.concurrency, a
// boolean for retry.jitter -- but always as part of a per-key constraint, so no
// numbered rule is about being readable as a type on its own. The one exception is
// R40, which is written over "every duration value" and is therefore reported by the
// duration wrapper itself rather than under this name (scalar.go).
const (
	RuleRead        RuleID = "read"
	RuleSyntax      RuleID = "syntax"
	RuleDocument    RuleID = "document"
	RuleInterpolate RuleID = "interpolate"
	RuleNormalize   RuleID = "normalize"
	RuleShape       RuleID = "shape"
	RuleDecode      RuleID = "decode"
)

// noRule is the absence of one. It is the zero RuleID rather than a value of its own, so a
// declaration that names no rule is indistinguishable from one that has not been written --
// which is what makes "this key is required" and "this key names the rule that reports it
// missing" one statement instead of two that can disagree.
const noRule RuleID = ""

// structuralRuleIDs is that set as a value, so the disjointness a coverage walker relies on
// is asserted over the set rather than over a copy of it that a sixth constant could be
// added behind.
var structuralRuleIDs = []RuleID{RuleRead, RuleSyntax, RuleDocument, RuleInterpolate, RuleNormalize,
	RuleShape, RuleDecode}

// Positioned is a location in the configuration source, expressed without any
// parser type. Rule code reads positions through this interface, so the rule layer
// never imports the YAML library.
//
// Its one implementation, sourcePos, lives in position.go beside the column derivation,
// because a position is defined by that derivation rather than by this file.
type Positioned interface {
	// File is the display name of the configuration file.
	File() string
	// Line is the 1-based source line, or zero when there is no position.
	Line() int
	// Col is the 1-based rune column of the offending token's first character.
	Col() int
	// Path locates the node, for example listeners[0].payload.columns.
	Path() string
}

// Error is one configuration diagnostic: what is wrong, where, and how to fix it.
type Error struct {
	// Rule names the rule that raised this diagnostic. It is not rendered.
	Rule RuleID
	// File, Line and Col locate the offending token. Line and Col are zero for a
	// diagnostic that has no position, such as a file that could not be read.
	File string
	Line int
	Col  int
	// Path locates the node, for example listeners[0].payload.columns.
	Path string
	// Msg states the violated rule.
	Msg string
	// Hint suggests a remediation, and is empty when there is nothing to suggest.
	Hint string
}

// Warning is a diagnostic that does not prevent a configuration from loading. It
// carries the same fields as Error and is ordered by the same keys, but is returned
// in its own collection so that a warnings-only load can still succeed.
type Warning Error

// NewError builds a positioned diagnostic. The RuleID is the first parameter
// because no diagnostic may exist without naming the rule that raised it.
func NewError(rule RuleID, at Positioned, msg string) Error {
	return Error{
		Rule: rule,
		File: at.File(),
		Line: at.Line(),
		Col:  at.Col(),
		Path: diagnosticPath(at.Path()),
		Msg:  msg,
	}
}

// NewFileError builds a diagnostic about the file as a whole, for conditions that
// have no token to anchor on: a file that cannot be read, and a file that holds no
// configuration at all.
func NewFileError(rule RuleID, file, msg string) Error {
	return Error{Rule: rule, File: file, Path: yamlPathRoot, Msg: msg}
}

// diagnosticPath gives file- and document-level findings the YAML document root. Positioned keeps
// returning the empty path for a root node, because that is the parser-independent path convention
// its callers consume; Error's public locator is stronger and always names something.
func diagnosticPath(path string) string {
	if path == "" {
		return yamlPathRoot
	}
	return path
}

// NewWarning builds a positioned non-fatal diagnostic.
func NewWarning(rule RuleID, at Positioned, msg string) Warning {
	return Warning(NewError(rule, at, msg))
}

// WithHint returns a copy of the diagnostic carrying a remediation hint.
func (e Error) WithHint(hint string) Error {
	e.Hint = hint
	return e
}

// WithHint returns a copy of the warning carrying a remediation hint.
func (w Warning) WithHint(hint string) Warning {
	w.Hint = hint
	return w
}

// Errors is a collection of diagnostics. A configuration loads only when it is
// empty.
type Errors []Error

// Warnings is a collection of non-fatal diagnostics. It may be non-empty for a
// configuration that loads successfully.
type Warnings []Warning

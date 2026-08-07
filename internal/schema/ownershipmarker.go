package schema

import "strings"

// This file is the ownership-marker contract as text: the two forms the Architecture's
// Ownership-marker contract block spells, built and read in one place so the two cannot come to
// disagree about their own prefix (skill Pattern 7).
//
//	schema     COMMENT ON SCHEMA "<schema>"          IS 'pg_noty:v1:<instance>'
//	partition  COMMENT ON TABLE  "<schema>"."<name>" IS 'pg_noty:v1:<instance>:partition'
//
// The marker is the mechanism behind every destructive guard, and its four states are behavioural
// rather than decorative (ADR-3). An **absent** marker is claimed, because a DBA pre-creating the
// schema is the supported minimal-privilege path and refusing it would break the path this package
// documents. A **foreign** marker is refused with ErrForeignInstance, because claiming another
// instance's schema is a two-service corruption. An **unreadable** marker is a third answer again,
// and defaulting it to either of the other two is a fail-open.
//
// Step 13 reads the schema form at boot; Step 11 writes the partition form on every partition it
// creates; Step 14 reads that same partition form inside the transaction that performs the drop.
// All three reach the catalog through ownershipclaim.go, which is the only half of this contract
// that holds a statement -- and, since that split, the only half that names a handle at all.

const (
	// markerVersion is the marker format's own version, separate from the schema version the ledger
	// records: this string changes when the marker's shape changes and not when a migration lands.
	markerVersion = "v1"
	// MarkerPrefix opens both forms.
	MarkerPrefix = "pg_noty:" + markerVersion + ":"
	// partitionMarkerSuffix closes the table form, which is what tells a partition of ours from the
	// schema holding it.
	partitionMarkerSuffix = ":partition"
)

// markerForm is one of the two shapes the contract spells. It is a value rather than a pair of
// string-building functions so that reading and writing a form are the same object's two methods
// and cannot drift apart.
type markerForm struct {
	suffix string
}

var (
	schemaMarkerForm    = markerForm{suffix: ""}
	partitionMarkerForm = markerForm{suffix: partitionMarkerSuffix}
)

// text is the marker one instance writes in this form.
func (form markerForm) text(instance string) string {
	return MarkerPrefix + instance + form.suffix
}

// instanceIn is the instance a comment names in this form, or a refusal to read it.
//
// A comment naming no instance at all -- the bare prefix, or the prefix and the suffix with nothing
// between them -- is refused rather than read as an empty instance name, because an empty name
// would compare equal to a Config that carried none and claim a schema on a technicality.
//
// A marker written under a later markerVersion fails the prefix and is therefore unreadable rather
// than foreign, which is the fail-closed answer: this reader cannot know what a later format puts
// after the prefix, so it must not claim to have read one.
func (form markerForm) instanceIn(written string) (string, bool) {
	named, opens := strings.CutPrefix(written, MarkerPrefix)
	if !opens {
		return "", false
	}
	named, closes := strings.CutSuffix(named, form.suffix)
	if !closes || named == "" {
		return "", false
	}
	return named, true
}

// markerState is what the catalog says about one object's ownership marker.
type markerState string

const (
	// markerOurs is this instance's own marker, read back verbatim.
	markerOurs markerState = "ours"
	// markerAbsent is no marker at all, which ADR-3 makes claimable.
	markerAbsent markerState = "absent"
	// markerUnreadable is a comment that is not a marker of this format.
	markerUnreadable markerState = "unreadable"
	// markerForeign is a marker of this format naming another instance, which ADR-3 refuses.
	markerForeign markerState = "foreign"
)

// markerStates is the declared set as a value, so a test quantifies over it and a fifth state has
// to join it. The zero markerState is deliberately none of them: a read that failed answers its
// zero value, and that must not be readable as a state the catalog reported.
var markerStates = []markerState{markerOurs, markerAbsent, markerUnreadable, markerForeign}

// markerReading is what one object's ownership marker says.
type markerReading struct {
	state markerState
	// named is the instance the marker names, and is empty unless the marker was readable. It is
	// carried rather than discarded because ErrForeignInstance has to name both instances --
	// errors.go's foreignInstance takes the marked one and the expected one -- and an operator
	// cannot tell a misconfigured instance name from two services sharing one schema without it.
	named string
}

// markerReadingOf is which of the four states one object's comment puts it in, and the instance it
// names.
//
// present is whether the object carries a comment at all, and it is a parameter rather than a test
// on written because the two questions differ. Measured on PostgreSQL 17.10, a comment set to the
// empty string is removed rather than stored, so today an absent comment is the only way to reach a
// text of "" -- but a version that stored one would hand this reader a present, unreadable marker,
// and that is what the empty-text row of the grid answers for.
func markerReadingOf(form markerForm, written string, present bool, instance string) markerReading {
	switch named, readable := form.instanceIn(written); {
	case !present:
		return markerReading{state: markerAbsent}
	case !readable:
		return markerReading{state: markerUnreadable}
	case named == instance:
		return markerReading{state: markerOurs, named: named}
	default:
		return markerReading{state: markerForeign, named: named}
	}
}

// markedObject is one object whose ownership marker can be read and written: the service schema, or
// one partition inside it. It is built by a pure constructor so that this file declares one
// error-returning read and one error-returning write rather than one of each per kind, and so that
// a caller cannot pair a schema's query with a partition's marker form.
//
// Its queries and its COMMENT ON forms are catalog.go's constants, read by name: that file is the
// package's only source that may name a system catalog, and a second copy of these strings here
// would break exactly that.
type markedObject struct {
	form     markerForm
	instance string
	// target is the object name identifier.go rendered, which both the read and the write carry.
	readQuery, commentOn, target string
}

// schemaObject is the service schema as a marked object (ADR-3's boot-time claim).
func schemaObject(schema, instance string) (markedObject, IdentifierFault) {
	target, fault := Quoted(schema)
	if fault != IdentifierOK {
		return markedObject{}, fault
	}
	return markedObject{form: schemaMarkerForm, instance: instance,
		readQuery: schemaMarkerQuery, commentOn: commentOnSchema, target: target}, IdentifierOK
}

// partitionObject is one partition of the event log as a marked object: what Step 11 writes on
// every partition it creates, and what Step 14's first drop guard reads back.
func partitionObject(schema, partition, instance string) (markedObject, IdentifierFault) {
	target, fault := Qualified(schema, partition)
	if fault != IdentifierOK {
		return markedObject{}, fault
	}
	return markedObject{form: partitionMarkerForm, instance: instance,
		readQuery: partitionMarkerQuery, commentOn: commentOnTable, target: target}, IdentifierOK
}

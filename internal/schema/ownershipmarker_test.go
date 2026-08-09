package schema

import (
	"slices"
	"strings"
	"testing"
)

// ourInstance is the instance every row below reads the marker on behalf of.
const ourInstance = "noty"

// markerFormRows and markerReadingRows are the two axes of one grid: every state, for every form.
// They are declared as values and crossed rather than transcribed one into the other, because a
// header naming two axes is audited by reading the header.
var markerFormRows = []struct {
	name string
	form markerForm
}{
	{name: "the service schema", form: schemaMarkerForm},
	{name: "a partition", form: partitionMarkerForm},
}

var markerReadingRows = []struct {
	name string
	// write builds this reading's comment from whichever form is under test.
	write   func(form markerForm) string
	present bool
	want    markerState
}{
	{name: "this instance's own marker", present: true, want: markerOurs,
		write: func(form markerForm) string { return form.text(ourInstance) }},
	{name: "no comment at all", present: false, want: markerAbsent,
		write: func(markerForm) string { return "" }},
	{name: "another instance's marker", present: true, want: markerForeign,
		write: func(form markerForm) string { return form.text("reporting") }},
	{name: "an instance whose name merely starts with ours", present: true, want: markerForeign,
		write: func(form markerForm) string { return form.text(ourInstance + "2") }},
	{name: "a note somebody wrote by hand", present: true, want: markerUnreadable,
		write: func(markerForm) string { return "created for the 2026 migration, do not drop" }},
	// Measured on PostgreSQL 17.10, COMMENT ON ... IS '' removes the comment rather than storing
	// one, so no catalog state reaches this row today. It is here because a version that stored one
	// would hand the reader a present marker of no readable form, and "present but empty" must not
	// fall through to absent -- which is the state ADR-3 claims a schema on.
	{name: "a present comment holding nothing", present: true, want: markerUnreadable,
		write: func(markerForm) string { return "" }},
	{name: "a marker of a later format version", present: true, want: markerUnreadable,
		write: func(form markerForm) string {
			return strings.Replace(form.text(ourInstance), markerVersion, "v2", 1)
		}},
	{name: "a marker naming no instance", present: true, want: markerUnreadable,
		write: func(form markerForm) string { return form.text("") }},
}

// TestEveryMarkerStateIsAnsweredForEveryForm crosses both axes, so a state answered only for the
// form that came to mind fails the row named for the other one. None of the four is a fallthrough:
// each row asserts its own answer, which is what keeps ADR-3's claim-versus-refuse pair
// implementable -- an absent marker is claimed because a DBA pre-creating the schema is the
// supported minimal-privilege path, and a foreign one is refused with ErrForeignInstance.
func TestEveryMarkerStateIsAnsweredForEveryForm(t *testing.T) {
	for _, form := range markerFormRows {
		for _, reading := range markerReadingRows {
			t.Run(form.name+", "+reading.name, func(t *testing.T) {
				written := reading.write(form.form)

				got := markerReadingOf(form.form, written, reading.present, ourInstance)
				if got.state != reading.want {
					t.Errorf("a %s carrying %q (present=%t) reads as %q, want %q",
						form.name, written, reading.present, got.state, reading.want)
				}
				assertNamesTheInstanceItsRefusalWillCite(t, got, written)
			})
		}
	}
}

// assertNamesTheInstanceItsRefusalWillCite is the clause ADR-3's refusal rests on: ErrForeignInstance
// names both instances, and errors.go's foreignInstance takes the marked one from here. A reading
// that could not be read names nobody, so a caller cannot cite a name the catalog never held.
func assertNamesTheInstanceItsRefusalWillCite(t *testing.T, got markerReading, written string) {
	t.Helper()

	switch got.state {
	case markerOurs, markerForeign:
		if got.named == "" || !strings.Contains(written, got.named) {
			t.Errorf("a %q marker read from %q names instance %q, which the marker does not carry",
				got.state, written, got.named)
		}
	default:
		if got.named != "" {
			t.Errorf("a %q marker read from %q names instance %q; nothing readable was found",
				got.state, written, got.named)
		}
	}
}

func TestTheMarkerGridIsCrossedOnBothAxesAndCoversEveryDeclaredState(t *testing.T) {
	if len(markerStates) != 4 {
		t.Fatalf("%d marker states are declared %v; update this count with the set",
			len(markerStates), markerStates)
	}
	if slices.Contains(markerStates, markerState("")) {
		t.Error("the zero markerState is one of the four, so a read that failed and answered its " +
			"zero value would be indistinguishable from a state the catalog reported")
	}

	if len(markerFormRows) != 2 {
		t.Fatalf("%d marker forms are crossed, want the two the contract spells", len(markerFormRows))
	}

	answered := map[markerState]int{}
	for _, reading := range markerReadingRows {
		answered[reading.want]++
	}
	for _, state := range markerStates {
		if answered[state] == 0 {
			t.Errorf("no row expects %q, so that state is answered by nothing and every form's "+
				"row for it is missing rather than merely unwritten", state)
		}
	}
}

// TestTheMarkerFormsSpellExactlyWhatTheContractSpells pins the two forms against the Architecture's
// Ownership-marker contract block rather than against the code that builds them: deriving the
// expectation from MarkerPrefix would pass for every prefix, including one a later edit changed.
func TestTheMarkerFormsSpellExactlyWhatTheContractSpells(t *testing.T) {
	if got := schemaMarkerForm.text(ourInstance); got != "pg_noty:v1:noty" {
		t.Errorf("the schema marker for instance %q is %q, want %q; the contract writes "+
			"COMMENT ON SCHEMA ... IS 'pg_noty:v1:<instance>'", ourInstance, got, "pg_noty:v1:noty")
	}
	if got := partitionMarkerForm.text(ourInstance); got != "pg_noty:v1:noty:partition" {
		t.Errorf("the partition marker for instance %q is %q, want %q; the contract writes "+
			"COMMENT ON TABLE ... IS 'pg_noty:v1:<instance>:partition'",
			ourInstance, got, "pg_noty:v1:noty:partition")
	}
}

// TestOneFormsMarkerIsNotReadAsTheOthers records what happens when a form meets the other form's
// text. The partition form refuses the schema form outright. The schema form reads the partition
// form as a foreign instance named "noty:partition", which is not a defect and is pinned so that it
// stays a decision: the two readers are applied to different kinds of object -- one to a schema and
// one to a table -- so no catalog state puts either text in front of the wrong one.
func TestOneFormsMarkerIsNotReadAsTheOthers(t *testing.T) {
	schemaText, partitionText := schemaMarkerForm.text(ourInstance), partitionMarkerForm.text(ourInstance)

	if got := markerReadingOf(partitionMarkerForm, schemaText, true, ourInstance).state; got != markerUnreadable {
		t.Errorf("a partition carrying the schema marker %q reads as %q, want %q",
			schemaText, got, markerUnreadable)
	}
	if got := markerReadingOf(schemaMarkerForm, partitionText, true, ourInstance).state; got != markerForeign {
		t.Errorf("a schema carrying the partition marker %q reads as %q, want %q",
			partitionText, got, markerForeign)
	}
}

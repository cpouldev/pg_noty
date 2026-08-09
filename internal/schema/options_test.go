package schema

import (
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"
)

// optionsField is one field the Contracts block declares on Options, by name and by type. The set is
// a value rather than a list written inline at one assertion, so the pin quantifies over it and a
// fourth field cannot be added unasserted (.claude/rules/assert-a-set-wide-invariant-over-the-set.md).
type optionsField struct{ name, typeName string }

// theContractsOptionsFields is transcribed from the task file's Contracts block:
//
//	Options{LockTimeout time.Duration, Stats *MaintenanceStats, Logger *slog.Logger}
//
// Steps 8, 11, 13, 14 and 15 all bind to this shape by name, so a divergence here is a compile error
// in five consumers rather than a silent defect -- which is exactly why it is pinned rather than
// trusted.
var theContractsOptionsFields = []optionsField{
	{name: "LockTimeout", typeName: "time.Duration"},
	{name: "Stats", typeName: "*schema.MaintenanceStats"},
	{name: "Logger", typeName: "*slog.Logger"},
}

// fieldSetIssues is every way one struct diverges from a declared field set: a different count, a
// field named differently at a position, or a field of another type. The real Options and all three
// synthetic controls below run through it, so a control cannot pass against a second expression of
// the same idea (.claude/rules/falsify-the-assertion-not-a-copy-of-it.md).
func fieldSetIssues(subject reflect.Type, want []optionsField) []string {
	if subject.NumField() != len(want) {
		return []string{fmt.Sprintf("%s declares %d fields, want %d",
			subject, subject.NumField(), len(want))}
	}

	var issues []string
	for i, declared := range want {
		found := subject.Field(i)
		if found.Name != declared.name || found.Type.String() != declared.typeName {
			issues = append(issues, fmt.Sprintf("field %d is %s %s, want %s %s",
				i, found.Name, found.Type, declared.name, declared.typeName))
		}
	}
	return issues
}

func TestOptionsCarriesExactlyTheThreeFieldsContractsNames(t *testing.T) {
	if len(theContractsOptionsFields) != 3 {
		t.Fatalf("%d fields are declared %v; the Contracts block names three, so update this count "+
			"with the set", len(theContractsOptionsFields), theContractsOptionsFields)
	}

	for _, issue := range fieldSetIssues(reflect.TypeOf(Options{}), theContractsOptionsFields) {
		t.Error(issue)
	}
}

// TestTheOptionsFieldPinRefusesEachDivergenceSeparately is the negative side of the same gate:
// today's Options can fail none of it, so each control writes a struct the contract does not
// declare. One clause per row, and the two shape rows keep the declared count so that only the
// name-and-type comparison can reject them -- a combined row cannot say which clause is still alive,
// and a pin that had stopped comparing names would read exactly like a passing one
// (.claude/rules/isolate-each-clause-of-a-multi-clause-guard.md).
func TestTheOptionsFieldPinRefusesEachDivergenceSeparately(t *testing.T) {
	for _, tc := range []struct {
		name    string
		subject reflect.Type
		want    string
	}{
		{
			name: "a fourth field the contract does not declare",
			subject: reflect.TypeOf(struct {
				LockTimeout time.Duration
				Stats       *MaintenanceStats
				Logger      *slog.Logger
				Extra       int
			}{}),
			want: "declares 4 fields, want 3",
		},
		{
			name: "a declared field under another name",
			subject: reflect.TypeOf(struct {
				Timeout time.Duration
				Stats   *MaintenanceStats
				Logger  *slog.Logger
			}{}),
			want: "field 0 is Timeout time.Duration, want LockTimeout time.Duration",
		},
		{
			name: "a declared field of another type",
			subject: reflect.TypeOf(struct {
				LockTimeout int64
				Stats       *MaintenanceStats
				Logger      *slog.Logger
			}{}),
			want: "field 0 is LockTimeout int64, want LockTimeout time.Duration",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues := fieldSetIssues(tc.subject, theContractsOptionsFields)

			if len(issues) != 1 || !strings.Contains(issues[0], tc.want) {
				t.Fatalf("the pin reported %v, want exactly one issue naming %q", issues, tc.want)
			}
		})
	}
}

// TestDefaultLockTimeoutIsTheValueContractsNames pins the constant against the literal the Contracts
// block writes, not against a predicate every plausible duration satisfies
// (.claude/rules/pin-specification-mandated-literals.md).
//
// D3: this step pins the literal, and Step 8 pins the same value behaviourally against a real
// lock_timeout holding a conflicting lock. Neither may cite the other's row, so changing this
// constant requires changing both.
func TestDefaultLockTimeoutIsTheValueContractsNames(t *testing.T) {
	if DefaultLockTimeout != 3*time.Second {
		t.Errorf("DefaultLockTimeout = %s, want %s; the Contracts block names the value",
			DefaultLockTimeout, 3*time.Second)
	}
}

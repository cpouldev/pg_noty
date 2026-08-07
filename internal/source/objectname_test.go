package source

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/schema"
)

func TestObjectNamesMatchTheSpecificationLiterals(t *testing.T) {
	for _, tc := range []struct{ operation, want string }{
		{"ins", "pg_noty_order_paid_ins"}, {"upd", "pg_noty_order_paid_upd"}, {"del", "pg_noty_order_paid_del"},
	} {
		if got := schema.ObjectName("pg_noty_order_paid", tc.operation); got != tc.want {
			t.Errorf("ObjectName(..., %q) = %q, want %q", tc.operation, got, tc.want)
		}
	}
}

// keptPrefixBytes is how much of the pre-truncation name survives truncation: the 63-byte limit
// less the eight-hex-digit tag and the underscore joining them, so 63 - 8 - 1 = 54. It is derived
// from that rule rather than transcribed, and it is the width at which two names become
// indistinguishable to a prefix cut.
const keptPrefixBytes = schema.MaxIdentifierBytes - 8 - 1

// hashedNameForm is what ObjectName writes when it has to truncate: the kept prefix, an
// underscore, and an eight-hex-digit tag, filling the 63-byte limit exactly.
var hashedNameForm = regexp.MustCompile(`^.{54}_[0-9a-f]{8}$`)

// TestObjectNamePinsThe63ByteBoundaryAndWholeNameHash names two claims, so it asserts both. The
// boundary rows carry the 63-byte equality case, which is the only input separating `len(full) <=
// 63` from `len(full) < 63`; each clause is checked on its own so a row cannot pass on the wrong
// one.
func TestObjectNamePinsThe63ByteBoundaryAndWholeNameHash(t *testing.T) {
	for _, tc := range []struct {
		name, base, suffix string
		truncated          bool
	}{
		{"NameAt62 one under the limit", strings.Repeat("a", 59), "bb", false},
		{"NameAt63 exactly the limit", strings.Repeat("a", 60), "bb", false},
		{"NameAt64 one over the limit", strings.Repeat("a", 60), "bbb", true},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				full, got := tc.base+"_"+tc.suffix, schema.ObjectName(tc.base, tc.suffix)
				if len(got) > schema.MaxIdentifierBytes {
					t.Fatalf("ObjectName = %q, which is %d bytes", got, len(got))
				}
				if !tc.truncated {
					if got != full {
						t.Fatalf("ObjectName = %q, want the whole %d-byte name %q", got, len(full), full)
					}
					return
				}
				assertHashedName(t, got, full)
			},
		)
	}

	// PrefixAt60 is the collision row: two names agreeing on their first 60 bytes and
	// differing at byte 61. Sixty is above the 54 bytes truncation keeps, so a prefix cut cannot
	// tell them apart and only a tag over the whole pre-truncation name can.
	t.Run(
		"PrefixAt60 shared prefix, differing at byte 61", func(t *testing.T) {
			shared := strings.Repeat("a", 60)
			assertTheTagKeepsThemApart(t, shared+"x"+strings.Repeat("z", 20), shared+"y"+strings.Repeat("z", 20))
		},
	)
	// The wider pair: agreeing on 71 bytes, so they also survive a naive cut to the 63-byte limit.
	assertTheTagKeepsThemApart(t, strings.Repeat("a", 71)+"x", strings.Repeat("a", 71)+"y")
}

// assertHashedName requires the hashed form rather than merely a shortened one. A naive cut to 63
// bytes is also at most 63 bytes and also differs from the whole name, so length and inequality
// together admit exactly the implementation M9 says collides.
func assertHashedName(t *testing.T, got, full string) {
	t.Helper()
	if got == full {
		t.Fatalf("ObjectName = %q, the whole %d-byte name", got, len(full))
	}
	if !hashedNameForm.MatchString(got) {
		t.Fatalf(
			"ObjectName = %q (%d bytes), want a 54-byte prefix, an underscore and an "+
				"eight-hex-digit tag; a plain cut to %d bytes has that length and no tag",
			got, len(got), schema.MaxIdentifierBytes,
		)
	}
	if want := full[:54]; !strings.HasPrefix(got, want) {
		t.Errorf("ObjectName = %q, which does not begin with the kept prefix %q", got, want)
	}
}

// assertTheTagKeepsThemApart is M9's own case: two names that agree on every byte a prefix cut can
// keep, so only a tag taken over the *whole* pre-truncation name can distinguish them. Both
// preconditions are asserted rather than assumed -- a pair short enough to escape truncation, or
// one differing inside the kept prefix, could not collide and so could not fail this. The width
// compared is the kept prefix, not the identifier limit: truncation keeps 54 bytes, so 54 is where
// two names become indistinguishable to a cut.
func assertTheTagKeepsThemApart(t *testing.T, leftBase, rightBase string) {
	t.Helper()
	leftFull, rightFull := leftBase+"_s", rightBase+"_s"
	switch {
	case len(leftFull) <= schema.MaxIdentifierBytes || len(rightFull) <= schema.MaxIdentifierBytes:
		t.Fatalf(
			"%q (%d bytes) and %q (%d bytes) are not both over the %d-byte limit, so neither "+
				"truncates and this pair could not collide",
			leftFull, len(leftFull), rightFull, len(rightFull), schema.MaxIdentifierBytes,
		)
	case leftFull[:keptPrefixBytes] != rightFull[:keptPrefixBytes]:
		t.Fatalf(
			"%q and %q differ within the %d bytes truncation keeps, so this pair could not "+
				"collide under a cut and the comparison below could not fail",
			leftFull, rightFull, keptPrefixBytes,
		)
	}

	left, right := schema.ObjectName(leftBase, "s"), schema.ObjectName(rightBase, "s")
	if left == right {
		t.Fatalf(
			"two names agreeing on their first %d bytes both resolve to %q; the tag is "+
				"derived from the truncated name rather than the whole one", keptPrefixBytes, left,
		)
	}
	if left[:keptPrefixBytes+1] != right[:keptPrefixBytes+1] {
		t.Errorf(
			"%q and %q differ before their tags, so their inequality is not evidence about "+
				"the tag at all", left, right,
		)
	}
}

// TestObjectNameIsDeterministicWithinOneProcess is the in-process determinism row. It is
// the weaker half of the pair below and is kept separate because a derivation seeded per call --
// from a map iteration, a random tag, a clock -- fails here without ever reaching a subprocess.
func TestObjectNameIsDeterministicWithinOneProcess(t *testing.T) {
	for _, tc := range []struct{ name, base, suffix string }{
		{"a name short enough to pass through whole", "pg_noty_order_paid", "ins"},
		{"a name the derivation has to truncate and tag", strings.Repeat("a", 60), "bbb"},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				first, second := schema.ObjectName(tc.base, tc.suffix), schema.ObjectName(tc.base, tc.suffix)
				if first != second {
					t.Fatalf("one input derived twice in one process gave %q then %q", first, second)
				}
			},
		)
	}
}

func TestObjectNameIsDeterministicAcrossProcesses(t *testing.T) {
	if os.Getenv("PG_NOTY_OBJECT_NAME_CHILD") == "1" {
		fmt.Print(schema.ObjectName(strings.Repeat("a", 60), "bbb"))
		return
	}
	want := schema.ObjectName(strings.Repeat("a", 60), "bbb")
	command := exec.Command(os.Args[0], "-test.run=TestObjectNameIsDeterministicAcrossProcesses")
	command.Env = append(os.Environ(), "PG_NOTY_OBJECT_NAME_CHILD=1")
	output, err := command.Output()
	got := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(string(output)), "PASS"))
	if err != nil || got != want {
		t.Fatalf("subprocess ObjectName = %q, err=%v; want %q", output, err, want)
	}
}

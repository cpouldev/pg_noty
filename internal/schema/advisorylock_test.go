package schema

import (
	"hash/fnv"
	"strconv"
	"testing"
)

// This file is SC-1. lockKey is arithmetic over two strings, so the whole of it is asserted without
// a container; what a container is needed for -- that the number it produces is one the server
// locks on, including on the negative half of the range -- is in advisorylock_integration_test.go.

// TestTheLockKeySeparatesInstancesAndSchemas is the row criterion 21 rests on, and its three
// neighbours.
//
// Criterion 21 says two configurations sharing a database and differing in `instance` must not
// serialise against each other, and that "a lock key that ignores `instance` fails this criterion".
// It fails it *two steps later*, in Step 13's concurrent-boot case, where the symptom is one boot
// waiting on another's lock for no reason -- so the instance-only row is here, where the defect is
// one line from the assertion rather than an emergent property of two processes.
//
// The last row is ADR-3's separator, which exists for a reason nothing else here would catch: a bare
// concatenation hashes ("not", "yorders") and ("noty", "orders") to one key, so two unrelated
// deployments would block each other and neither operator could see why.
func TestTheLockKeySeparatesInstancesAndSchemas(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		leftSchema, leftInstance   string
		rightSchema, rightInstance string
		wantSame                   bool
		why                        string
	}{
		{
			name:       "two configurations differing only in instance",
			leftSchema: "noty", leftInstance: "orders",
			rightSchema: "noty", rightInstance: "billing",
			why: "criterion 21: two instances sharing a schema must not serialise against each other",
		},
		{
			name:       "two configurations differing only in schema",
			leftSchema: "noty_orders", leftInstance: "orders",
			rightSchema: "noty_billing", rightInstance: "orders",
			why: "two schemas are two resources, and one key for both would serialise unrelated work",
		},
		{
			name:       "the same pair twice",
			leftSchema: "noty", leftInstance: "orders",
			rightSchema: "noty", rightInstance: "orders",
			wantSame: true,
			why:      "two replicas of one instance agree on the key or neither ever blocks the other",
		},
		{
			name:       "two pairs whose parts concatenate alike",
			leftSchema: "not", leftInstance: "yorders",
			rightSchema: "noty", rightInstance: "orders",
			why: "ADR-3's separator is what keeps these apart; a bare concatenation gives them one key",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			left := lockKey(tc.leftSchema, tc.leftInstance)
			right := lockKey(tc.rightSchema, tc.rightInstance)

			if same := left == right; same != tc.wantSame {
				t.Errorf("lockKey(%q, %q) = %d and lockKey(%q, %q) = %d; want them %s, because %s",
					tc.leftSchema, tc.leftInstance, left, tc.rightSchema, tc.rightInstance, right,
					sameOrDifferent(tc.wantSame), tc.why)
			}
		})
	}
}

// sameOrDifferent words the expectation the way the row states it.
func sameOrDifferent(wantSame bool) string {
	if wantSame {
		return "equal"
	}
	return "different"
}

// TestTheLockKeyIsTheHashADR3Names pins the derivation against the decision rather than against a
// number read off a run: ADR-3 names `fnv1a64(schema + "\x00" + instance)` verbatim, so the
// expectation is that expression and not a recorded digest.
//
// Without it every row above is satisfied by any injective function of the pair, including one that
// swapped the arguments -- and two replicas of one instance computing the key from different
// argument orders would each hold a lock the other never waits on.
//
// It states one thing more since lockKeyIn gained a domain, and states it the only way that can
// fail: comparing lockKey against lockKeyIn(bootstrapDomain, ...) compares a one-line wrapper with
// its own body, so it holds for every lockKeyIn ever written, including a swapped one. The
// derivation below fails unless bootstrapDomain contributes no bytes of its own.
func TestTheLockKeyIsTheHashADR3Names(t *testing.T) {
	const schema, instance = "noty", "orders"

	digest := fnv.New64a()
	if _, err := digest.Write([]byte(schema + "\x00" + instance)); err != nil {
		t.Fatalf("hash the pair: %v", err)
	}

	if got, want := lockKey(schema, instance), int64(digest.Sum64()); got != want {
		t.Errorf("lockKey(%q, %q) = %d, want %d: ADR-3 names fnv1a64 over the schema, a NUL and "+
			"the instance, in that order, under a bootstrap domain adding nothing to it",
			schema, instance, got, want)
	}
}

// TestSomeConfigurationsHashToANegativeKey is what makes the integration half's negative-key case a
// real one. A 64-bit digest with its high bit set converts to a negative int64, and the server's
// advisory-lock argument is a signed bigint, so half the key space is negative and nothing in the
// arithmetic may treat that as an error.
//
// It fails rather than skipping when no pair in the sweep is negative, because a search that found
// nothing and said nothing would leave the integration case testing the positive half twice.
func TestSomeConfigurationsHashToANegativeKey(t *testing.T) {
	if _, instance := aNegativelyKeyedInstance(t); instance == "" {
		t.Fatal("the search reported no pair, which it cannot do without failing first")
	}
}

// aNegativelyKeyedInstance is a (schema, instance) pair whose key falls in the negative half of the
// bigint range, found by walking a deterministic sequence rather than by writing down a pair some
// earlier run happened to produce -- a recorded pair stops being negative the moment the hash
// changes, and does so silently.
//
// The search is wide because a sequence of names is not a random sample, not because the outcome is
// rare: measured over 100000 names in this shape, 49890 of them key negatively, and this particular
// sequence still takes more than a thousand steps to reach its first.
func aNegativelyKeyedInstance(t *testing.T) (schema, instance string) {
	t.Helper()

	const searched = 4096
	for candidate := range searched {
		named := "instance-" + strconv.Itoa(candidate)
		if lockKey(harnessSchema, named) < 0 {
			return harnessSchema, named
		}
	}

	t.Fatalf("none of the first %d instance names under schema %q keys negatively, so half the "+
		"bigint range is untested; widen the search or re-derive the hash", searched, harnessSchema)
	return "", ""
}

package config

import (
	"strings"
	"testing"
)

// The one wrapper whose value is a collection, and the three things that makes different: an element
// carries a position of its own, an element it cannot read is refused on that element rather than on the
// list, and an explicitly empty list is a written value rather than an omission.

// TestAListElementCarriesItsOwnPosition is ADR-6's element class at the wrapper. A rule about a
// list's contents anchors on the element at fault, so the list's own position is not enough: without
// this, Step 9's nine list-content diagnostics would all point at the list.
func TestAListElementCarriesItsOwnPosition(t *testing.T) {
	src, node := nodeAt(t, "columns:\n  - id\n  - status\n", "$.columns")

	var held StrList
	got := readInto(t, src, &held, node, func() string { return strings.Join(textsIn(held), ",") })

	if len(got.diags) != 0 {
		t.Fatalf("recorded %q for a list of scalars", messagesOf(got.diags))
	}
	if len(held.values) != 2 {
		t.Fatalf("read %d elements, want 2", len(held.values))
	}
	// `  - ` is four runes, so each element begins at rune 5 of its own line.
	for i, want := range []int{2, 3} {
		if held.values[i].Line() != want || held.values[i].Col() != 5 {
			t.Errorf("element %d is at %d:%d, want %d:5", i, held.values[i].Line(), held.values[i].Col(), want)
		}
	}
}

// TestAnUnreadableListElementIsRefusedOnItselfRatherThanOnTheList is the element half of the
// never-fail guarantee, and it is the one StrList failure a document can actually reach: stage F
// judges that `columns` is a list and says nothing about what the list holds, so a nested container
// arrives here.
//
// Two of them, because the policy is to accumulate: a list holding two unusable elements names both
// in one run rather than sending its author back for the second.
func TestAnUnreadableListElementIsRefusedOnItselfRatherThanOnTheList(t *testing.T) {
	src, node := nodeAt(t, "columns: [id, [nested], {a: 1}]\n", "$.columns")

	var held StrList
	got := readInto(t, src, &held, node, func() string { return strings.Join(textsIn(held), ",") })

	// One expectation per diagnostic, in order, rather than a membership test over both columns. A
	// membership test is satisfied by both carets landing on the *same* element, which is the one thing
	// this test is named for ruling out: anchoring every element refusal on the first unreadable element
	// reports 1:15 twice and passed the whole suite.
	//
	// `columns: [id, ` is fourteen runes, so the second element begins at rune 15; `[nested], ` is ten
	// more, so the third begins at rune 25.
	wantColumns := []int{15, 25}

	if len(got.diags) != len(wantColumns) {
		t.Fatalf("recorded %d diagnostics %q, want one per unusable element", len(got.diags), messagesOf(got.diags))
	}
	for i, want := range wantColumns {
		if got.diags[i].Col != want {
			t.Errorf("diagnostic %d is at column %d, want element %d's own %d", i, got.diags[i].Col, i+2, want)
		}
	}
	if got.valid {
		t.Error("Valid() is true for a list holding an element it could not read")
	}
	if names := textsIn(held); len(names) != 1 || names[0] != "id" {
		t.Errorf("kept %q, want only the element it could read", names)
	}
}

// TestAnEmptyListIsDistinguishableFromAbsence is the boundary for the two container wrappers, and
// the contract is asymmetric across it on purpose: an empty `signing.secrets` says "sign nothing"
// and is valid, while absence means "no signing block was written at all". A wrapper that answered
// Set == false for `[]` would collapse the two.
func TestAnEmptyListIsDistinguishableFromAbsence(t *testing.T) {
	tests := []struct {
		name      string
		written   string
		wantSet   bool
		wantCount int
	}{
		{name: "an absent list", written: "", wantSet: false},
		{name: "an explicitly empty list", written: "        secrets: []\n", wantSet: true},
		{name: "a written list", written: "        secrets: [a, b]\n", wantSet: true, wantCount: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := decodedSigning(t, tc.written)

			secrets := raw.Destination.Value.Signing.Value.Secrets
			if secrets.Set != tc.wantSet {
				t.Errorf("Set = %t, want %t", secrets.Set, tc.wantSet)
			}
			if len(secrets.values) != tc.wantCount {
				t.Errorf("holds %d elements, want %d", len(secrets.values), tc.wantCount)
			}
		})
	}
}

// decodedSigning is the same for a signing block written inside the listener's destination.
func decodedSigning(t *testing.T, secretLines string) rawListener {
	t.Helper()

	destination := "    destination:\n      url: https://hooks.example.test/order-paid\n"
	if secretLines != "" {
		destination += "      signing:\n" + secretLines
	}
	return oneDecodedListener(t, aListenerOf(requiredName, requiredTable, requiredOperations, destination))
}

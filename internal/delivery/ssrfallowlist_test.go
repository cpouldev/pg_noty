package delivery

import (
	"net"
	"testing"
)

// TestAnExemptedRangeIsAdmittedAndNothingElseIs is the allow-list's two directions. An operator
// whose destinations live in private space -- a container network, an internal mesh -- names the
// exact prefix, and only that prefix is admitted: an exemption that widened to its neighbours, or to
// private space generally, would satisfy the first row and fail the two after it.
func TestAnExemptedRangeIsAdmittedAndNothingElseIs(t *testing.T) {
	// A /24 inside CGNAT, deliberately narrower than the /10 the guard blocks, so that an address
	// just outside the exemption is still inside a blocked range. Exempting the whole /10 would make
	// every near-miss publicly routable and the widening rows below unfalsifiable.
	exempt, err := ParseAllowedDestinations([]string{"100.64.0.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	control := ssrfControlAllowing(exempt)
	for _, testCase := range []struct {
		ip          string
		wantRefusal bool
	}{
		// 100.64.0.0/24 spans 100.64.0.0-100.64.0.255.
		{ip: "100.64.0.4", wantRefusal: false},     // the demo's echo server
		{ip: "100.64.0.255", wantRefusal: false},   // the last address the exemption covers
		{ip: "100.64.1.0", wantRefusal: true},      // the first past it, still CGNAT and still blocked
		{ip: "127.0.0.1", wantRefusal: true},       // a different blocked range is untouched
		{ip: "169.254.169.254", wantRefusal: true}, // the metadata address stays refused
	} {
		t.Run(testCase.ip, func(t *testing.T) {
			err := control("tcp", net.JoinHostPort(testCase.ip, "443"), nil)
			if refused := err != nil; refused != testCase.wantRefusal {
				t.Fatalf("control(%s) refused=%t (%v), want refused=%t", testCase.ip, refused, err, testCase.wantRefusal)
			}
		})
	}
	// Without an exemption the same address is refused, or the block below is untested.
	if err := ssrfSafeControl("tcp", net.JoinHostPort("100.64.0.4", "443"), nil); err == nil {
		t.Error("100.64.0.4 was admitted with no exemption; CGNAT is reachable inside several cloud providers")
	}
}

// TestAnUnparseableExemptionRefusesTheWholeSet holds the fail-closed direction: one bad entry must
// not leave the others quietly in force, because a half-applied allow-list reads as a working guard.
func TestAnUnparseableExemptionRefusesTheWholeSet(t *testing.T) {
	if networks, err := ParseAllowedDestinations([]string{"100.64.0.0/10", "not-a-cidr"}); err == nil {
		t.Fatalf("ParseAllowedDestinations accepted a malformed entry and returned %v", networks)
	}
}

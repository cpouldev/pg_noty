package delivery

import (
	"strings"
	"testing"
)

func TestCheckPayloadSizeEqualityAndNoMutation(t *testing.T) {
	const limit = 8
	for _, tc := range []struct {
		name string
		size int
		ok   bool
	}{
		{"below", limit - 1, true}, {"equal", limit, true}, {"above", limit + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := []byte(strings.Repeat("x", tc.size))
			before := string(payload)
			ok, reason := checkPayloadSize(payload, limit)
			if ok != tc.ok {
				t.Fatalf("size %d eligible = %t, want %t", tc.size, ok, tc.ok)
			}
			if string(payload) != before {
				t.Fatal("size guard mutated payload")
			}
			if !tc.ok && (!strings.Contains(reason, "9") || !strings.Contains(reason, "8")) {
				t.Fatalf("reason %q does not name measured size and limit", reason)
			}
			if tc.ok && reason != "" {
				t.Fatalf("eligible payload returned terminal reason %q", reason)
			}
		})
	}
}

func TestCheckPayloadSizeHandlesEmptyAndNegativeLimit(t *testing.T) {
	if ok, reason := checkPayloadSize(nil, 0); !ok || reason != "" {
		t.Fatalf("empty payload at zero = %t,%q", ok, reason)
	}
	if ok, reason := checkPayloadSize([]byte("x"), -1); ok || !strings.Contains(reason, "1") || !strings.Contains(reason, "-1") {
		t.Fatalf("negative limit result = %t,%q", ok, reason)
	}
}

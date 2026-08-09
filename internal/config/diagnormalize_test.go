package config

import (
	"fmt"
	"strings"
	"testing"
)

// fingerprint renders everything about a diagnostic set except Rule. Comparing
// two fingerprints byte-for-byte is how the tests below prove that Rule cannot
// influence de-duplication or ordering.
func fingerprint(diags Errors) string {
	var b strings.Builder
	for _, d := range diags {
		fmt.Fprintf(&b, "%s|%d|%d|%s|%s|%s\n", d.File, d.Line, d.Col, d.Path, d.Msg, d.Hint)
	}
	return b.String()
}

func TestErrorsDedupeOnFileLineColMsg(t *testing.T) {
	tests := []struct {
		name  string
		input Errors
		want  string
	}{
		{
			name: "identical file line col and msg collapse to one entry",
			input: Errors{
				{Rule: R17, File: "a.yaml", Line: 3, Col: 5, Path: "x", Msg: "boom"},
				{Rule: R17, File: "a.yaml", Line: 3, Col: 5, Path: "x", Msg: "boom"},
			},
			want: "a.yaml|3|5|x|boom|\n",
		},
		{
			name: "a differing message is a distinct diagnostic at the same position",
			input: Errors{
				{Rule: R17, File: "a.yaml", Line: 3, Col: 5, Path: "x", Msg: "first"},
				{Rule: R17, File: "a.yaml", Line: 3, Col: 5, Path: "x", Msg: "second"},
			},
			want: "a.yaml|3|5|x|first|\na.yaml|3|5|x|second|\n",
		},
		{
			name: "a differing path does not save a duplicate message at one position",
			input: Errors{
				{Rule: R17, File: "a.yaml", Line: 3, Col: 5, Path: "x", Msg: "boom"},
				{Rule: R17, File: "a.yaml", Line: 3, Col: 5, Path: "y", Msg: "boom"},
			},
			want: "a.yaml|3|5|x|boom|\n",
		},
		{
			name: "two listeners aliasing one broken anchor report once",
			input: Errors{
				{Rule: R36, File: "a.yaml", Line: 9, Col: 12, Path: "listeners[0].destination.url", Msg: "bad scheme"},
				{Rule: R36, File: "a.yaml", Line: 9, Col: 12, Path: "listeners[1].destination.url", Msg: "bad scheme"},
			},
			want: "a.yaml|9|12|listeners[0].destination.url|bad scheme|\n",
		},
		{
			name: "a differing hint does not make a second diagnostic",
			input: Errors{
				{Rule: R41, File: "a.yaml", Line: 3, Col: 5, Path: "x", Msg: "boom", Hint: "first"},
				{Rule: R41, File: "a.yaml", Line: 3, Col: 5, Path: "x", Msg: "boom", Hint: "second"},
			},
			want: "a.yaml|3|5|x|boom|first\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := fingerprint(tc.input.normalized()); got != tc.want {
				t.Errorf("normalized() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestErrorsSortOnFileLineColPath(t *testing.T) {
	tests := []struct {
		name  string
		input Errors
		want  string
	}{
		{
			name: "file wins over line",
			input: Errors{
				{Rule: R1, File: "b.yaml", Line: 1, Msg: "m"},
				{Rule: R1, File: "a.yaml", Line: 9, Msg: "m"},
			},
			want: "a.yaml|9|0||m|\nb.yaml|1|0||m|\n",
		},
		{
			name: "line wins over column",
			input: Errors{
				{Rule: R1, File: "a.yaml", Line: 9, Col: 1, Msg: "m"},
				{Rule: R1, File: "a.yaml", Line: 2, Col: 40, Msg: "m"},
			},
			want: "a.yaml|2|40||m|\na.yaml|9|1||m|\n",
		},
		{
			name: "column wins over path",
			input: Errors{
				{Rule: R1, File: "a.yaml", Line: 2, Col: 40, Path: "aaa", Msg: "m"},
				{Rule: R1, File: "a.yaml", Line: 2, Col: 3, Path: "zzz", Msg: "m"},
			},
			want: "a.yaml|2|3|zzz|m|\na.yaml|2|40|aaa|m|\n",
		},
		{
			name: "path breaks a tie at one position",
			input: Errors{
				{Rule: R1, File: "a.yaml", Line: 2, Col: 3, Path: "zzz", Msg: "m1"},
				{Rule: R1, File: "a.yaml", Line: 2, Col: 3, Path: "aaa", Msg: "m2"},
			},
			want: "a.yaml|2|3|aaa|m2|\na.yaml|2|3|zzz|m1|\n",
		},
		{
			name: "message breaks a tie the declared key leaves open",
			input: Errors{
				{Rule: R1, File: "a.yaml", Line: 2, Col: 3, Path: "p", Msg: "zebra"},
				{Rule: R1, File: "a.yaml", Line: 2, Col: 3, Path: "p", Msg: "apple"},
			},
			want: "a.yaml|2|3|p|apple|\na.yaml|2|3|p|zebra|\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := fingerprint(tc.input.normalized()); got != tc.want {
				t.Errorf("normalized() =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

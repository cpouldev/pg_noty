package config

import "testing"

func TestUnreadableFlowKeyDetectionDistinguishesKeyAndValuePositions(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{name: "first alias key", line: `outer: {*alias: secret}`, want: true},
		{name: "later alias key", line: `outer: {first: value, *alias : secret}`, want: true},
		{name: "nested alias key", line: `outer: {nested: {*alias: secret}}`, want: true},
		{name: "mapping in sequence", line: `outer: {items: [{*alias: secret}]}`, want: true},
		{name: "anchor key property", line: `outer: {&named ordinary: secret}`, want: true},
		{name: "tagged key property", line: `outer: {!!str ordinary: secret}`, want: true},
		{name: "explicit complex key", line: `outer: {? [a, b] : secret}`, want: true},
		{name: "first alias value", line: `outer: {first: *alias}`, want: false},
		{name: "later alias value", line: `outer: {first: one, second: *alias}`, want: false},
		{name: "alias sequence value", line: `outer: {values: [*alias, other]}`, want: false},
		{name: "quoted alias text", line: `outer: {"*alias: text": public}`, want: false},
		{name: "comment alias text", line: `outer: {first: value} # {*alias: text}`, want: false},
		{name: "ordinary keys", line: `outer: {first: one, nested: {second: two}}`, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := containsUnreadableFlowKey(tc.line); got != tc.want {
				t.Errorf("containsUnreadableFlowKey(%q) = %t, want %t", tc.line, got, tc.want)
			}
		})
	}
}

func TestLeadingTokenSeparatesUnreadableKeyIndicatorsFromAliasValues(t *testing.T) {
	tests := []struct {
		name string
		line string
		want leadingToken
	}{
		{name: "alias key", line: `*alias : secret`, want: aKeyThisFileCannotRead},
		{name: "sequence alias key", line: `- *alias : secret`, want: aKeyThisFileCannotRead},
		{name: "anchor key", line: `&named ordinary : secret`, want: aKeyThisFileCannotRead},
		{name: "tagged key", line: `!!str ordinary : secret`, want: aKeyThisFileCannotRead},
		{name: "complex key", line: `? [a, b] : secret`, want: aKeyThisFileCannotRead},
		{name: "alias value", line: `ordinary: *alias`, want: aKeyThisFileCanRead},
		{name: "alias alone", line: `*alias`, want: noKeyAtAll},
		{name: "tagged URI value", line: `!!str http://host/x`, want: noKeyAtAll},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := leadingTokenOf(tc.line); got != tc.want {
				t.Errorf("leadingTokenOf(%q) = %d, want %d", tc.line, got, tc.want)
			}
		})
	}
}

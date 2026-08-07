package source

import "testing"

func TestDollarQuoteTagAscendsOverRenderedBodies(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{name: "no collision", body: "BEGIN NULL; END", want: "fn"},
		{name: "base collision", body: "BEGIN RAISE NOTICE '$fn$'; END", want: "fn_1"},
		{name: "two collisions", body: "'$fn$' and '$fn_1$'", want: "fn_2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dollarQuoteTag(tc.body); got != tc.want {
				t.Errorf("dollarQuoteTag(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

func TestDollarQuoteTagIsDeterministic(t *testing.T) {
	body := `"a$b" and '$fn$'`
	first, second := dollarQuoteTag(body), dollarQuoteTag(body)
	if first != "fn_1" || second != first {
		t.Fatalf("dollarQuoteTag(%q) = %q then %q, want fn_1 twice", body, first, second)
	}
}

func TestDollarQuoteTagIsLexicallyBlindToEnclosingQuotes(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{name: "dollar in quoted identifier", body: `SELECT "a$b"`, want: "fn"},
		{name: "tag in string literal", body: `SELECT '$fn$'`, want: "fn_1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dollarQuoteTag(tc.body); got != tc.want {
				t.Errorf("dollarQuoteTag(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

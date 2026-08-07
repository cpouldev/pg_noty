package config

import "testing"

// What makes two keys the same key when the merge expansion asks whether the author already
// wrote one. It is one question about a key on its own, so it is asked here rather than beside
// the expansion that asks it (mergekey_test.go) or beside what a `<<` may point at
// (mergesource_test.go).

// textualKeySpellings is every way the contract can write one name. All must be one key, or a
// merge would smuggle in a second `url` written as `'url'` past the direct key that spells it.
var textualKeySpellings = map[string]string{
	"a bare key":          "key: v\n",
	"a single-quoted key": "'key': v\n",
	"a double-quoted key": "\"key\": v\n",
	"an explicit key":     "? key\n: v\n",
	"an anchored key":     "&k key: v\n",
	"a tagged key":        "!!str key: v\n",
}

// nonTextualKeySpellings is the same wrappers crossed with a key the parser read as something
// other than text -- the crossing the guard is decided by.
//
// Every one of them carries no text at all, so an identity that asks whether the node *as
// written* is a non-textual scalar answers "no" for the three wrapped rows and identifies them
// by their empty text. That makes `? 16`, `&k 16`, `!!int 16` and the empty-string key one key,
// and a merged mapping then holds the first of them and silently drops the rest -- the
// silent-wrong-configuration failure this stage exists to prevent.
var nonTextualKeySpellings = map[string]string{
	"a bare integer key":      "16: v\n",
	"an explicit integer key": "? 16\n: v\n",
	"an anchored integer key": "&k 16: v\n",
	"a tagged integer key":    "!!int 16: v\n",
}

// nonTextualKeyRenderings is the other dimension of the same guard: one value per row, written
// every way this library version still reads as that value.
//
// It is the dimension the wrapper set cannot reach. `16` and `0x10` are one key -- YAML resolves
// both to the integer sixteen -- but they are two renderings, so an identity built from a node's
// own text keeps them apart and a merge brings in a second spelling of a key the target already
// holds. Measured on v1.19.2 before the value read was written: every row below but the null one
// identified as several keys, and `use: {16: direct, <<: *b}` merging `0x10: from-merge`
// decoded to the *merged* value -- the direct key silently losing, which is the failure V5 is
// pinned for.
//
// Every spelling here was measured to parse as the kind it claims: `15e-1` and `yes` are absent
// because this version reads them as strings.
var nonTextualKeyRenderings = map[string][]string{
	"the null value":         {"~: v\n", "null: v\n", "Null: v\n", "NULL: v\n"},
	"the integer sixteen":    {"16: v\n", "0x10: v\n", "0o20: v\n", "020: v\n", "+16: v\n", "1_6: v\n"},
	"the integer minus one":  {"-1: v\n", "-0x1: v\n"},
	"the float one and half": {"1.5: v\n", "1.50: v\n", "1.5e0: v\n", "+1.5: v\n"},
	"the boolean true":       {"true: v\n", "True: v\n", "TRUE: v\n"},
	"positive infinity":      {".inf: v\n", ".Inf: v\n", ".INF: v\n"},
	"not a number":           {".nan: v\n", ".NaN: v\n"},
}

// identityOf is the identity of the key the document's first entry writes.
func identityOf(t *testing.T, document string) string {
	t.Helper()

	return keyIdentity(firstEntryOf(t, parsedRoot(t, document)).Key)
}

// TestKeysAreTheSameKeyInEverySpellingTheContractCanWriteThem covers the textual half: one name
// is one key however it is written (Step 3's Implementation Note 16).
func TestKeysAreTheSameKeyInEverySpellingTheContractCanWriteThem(t *testing.T) {
	bare := identityOf(t, "key: v\n")

	for name, document := range textualKeySpellings {
		t.Run(name, func(t *testing.T) {
			if got := identityOf(t, document); got != bare {
				t.Errorf("identity is %q, want %q: the same key written another way", got, bare)
			}
		})
	}
}

// TestANonTextualKeyIsTheSameKeyInEverySpellingOfIt covers the other half. It is the half the
// wrappers decide: the text is empty for all four rows, so the identity has to come from the
// node the text was read from rather than from the node as written.
func TestANonTextualKeyIsTheSameKeyInEverySpellingOfIt(t *testing.T) {
	bare := identityOf(t, "16: v\n")

	for name, document := range nonTextualKeySpellings {
		t.Run(name, func(t *testing.T) {
			if got := identityOf(t, document); got != bare {
				t.Errorf("identity is %q, want %q: the same integer key written another way", got, bare)
			}
		})
	}
}

// TestOneNonTextualValueIsOneKeyInEveryRenderingOfIt is the dimension the wrappers cannot reach.
// A rendering is how the author typed the value; the identity has to be the value itself, or a
// merge adds a second spelling of a key the target already holds.
func TestOneNonTextualValueIsOneKeyInEveryRenderingOfIt(t *testing.T) {
	for name, renderings := range nonTextualKeyRenderings {
		t.Run(name, func(t *testing.T) {
			first := identityOf(t, renderings[0])

			for _, document := range renderings[1:] {
				if got := identityOf(t, document); got != first {
					t.Errorf("%q identifies as %q, want %q: the same value written another way",
						document, got, first)
				}
			}
		})
	}
}

// TestNoTwoNonTextualValuesShareOneIdentity is the other side of the same guard. Reading the
// parsed value rather than the rendering can only merge identities, so the merging has to stop
// where YAML's own does: sixteen is not minus one, `+1.5` is not `1.5e0`'s integer neighbour, and
// `.inf` is not `-.inf`.
func TestNoTwoNonTextualValuesShareOneIdentity(t *testing.T) {
	distinct := map[string]string{
		"sixteen":            "16: v\n",
		"minus one":          "-1: v\n",
		"one and a half":     "1.5: v\n",
		"the float sixteen":  "16.0: v\n",
		"positive infinity":  ".inf: v\n",
		"negative infinity":  "-.inf: v\n",
		"not a number":       ".nan: v\n",
		"true":               "true: v\n",
		"false":              "false: v\n",
		"null":               "~: v\n",
		"the string sixteen": "\"16\": v\n",
	}

	assertNoTwoDocumentsShareAnIdentity(t, distinct)
}

// TestTheMergeMarkerIdentifiesAsAKeyOfItsOwn reaches the one arm of the identity that no
// expansion reaches: `<<` holds no value the parser reduced, so it falls back to its rendering.
//
// No document brings it here through the expansion -- a merge entry is dropped by the mapping
// that writes it, and a source is reduced before its keys are read -- so the arm is reached
// directly, and what it must answer is asserted rather than merely reached. The claim is the one
// that matters: the marker is not the ordinary key `"<<"`, which YAML says is a string like any
// other.
func TestTheMergeMarkerIdentifiesAsAKeyOfItsOwn(t *testing.T) {
	merged := firstEntryOf(t, valueOfEntry(parsedRoot(t, "a: &a {p: 1}\nb:\n  <<: *a\n"), 1)).Key

	if !merged.IsMergeKey() {
		t.Fatalf("the nested mapping writes %T, want the merge marker; this would assert nothing", merged)
	}
	if got := keyIdentity(merged); got == identityOf(t, "\"<<\": v\n") {
		t.Errorf("the marker and the quoted key both identify as %q; YAML keeps the two apart", got)
	}
}

// distinctKeyKinds is one document per kind of key the contract can write, each of which YAML
// keeps apart from all the others. The empty-string key is here because it is the row a guard
// that reads empty text as "no text" collides everything else with.
var distinctKeyKinds = map[string]string{
	"the name key":      "key: v\n",
	"the empty string":  "\"\": v\n",
	"the string \"16\"": "\"16\": v\n",
	"the integer 16":    "16: v\n",
	"the float 1.5":     "1.5: v\n",
	"the boolean true":  "true: v\n",
	"the null key":      "~: v\n",
}

// TestNoTwoKindsOfKeyShareOneIdentity is the invariant over the set rather than one pair of
// it: each kind is its own key, so a merge cannot drop one of two keys the document keeps
// apart.
//
// Together with the two spelling cases above this closes the identity: every spelling of a kind
// collapses onto that kind's own identity, and no two kinds collapse onto each other.
func TestNoTwoKindsOfKeyShareOneIdentity(t *testing.T) {
	assertNoTwoDocumentsShareAnIdentity(t, distinctKeyKinds)
}

// assertNoTwoDocumentsShareAnIdentity is the shape both separation claims take: keys the document
// keeps apart must stay apart, whether they differ in kind or only in value.
func assertNoTwoDocumentsShareAnIdentity(t *testing.T, documents map[string]string) {
	t.Helper()

	seen := make(map[string]string, len(documents))
	for name, document := range documents {
		identity := identityOf(t, document)

		if other, collides := seen[identity]; collides {
			t.Errorf("%s and %s both identify as %q, so a merge would drop one of two keys",
				name, other, identity)
		}
		seen[identity] = name
	}
}

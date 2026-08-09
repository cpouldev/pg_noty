package config

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// This file is the leak the node-shape family was written for, kept independently reproducible
// beside its rows in the grid: a value the schema declares sensitive in full, written in a shape the
// parser reads as a block mapping, rendered its first key in the clear.
//
// The shape of the failure is what makes it worth its own file. The diagnostic that leaked is the
// one reporting the very mis-shaping that defeated the redaction, so the leak fired exactly on the
// input class that produces a diagnostic at all -- and on the path-aware branch, which is the branch
// every well-formed configuration takes.

// wellFormedThrough is an otherwise valid configuration whose signing secrets are written by the
// caller. Everything else is present and correct, so any diagnostic the run produces is about the
// secrets and the document is on the path-aware branch by construction.
func wellFormedThrough(secrets string) string {
	return "version: 1\n" +
		"database:\n" +
		"  url: postgres://noty:hunter2@db.internal:5432/noty\n" +
		"  schema: noty\n" +
		"listeners:\n" +
		"  - name: order_paid\n" +
		"    table: public.orders\n" +
		"    operations:\n" +
		"      insert: {}\n" +
		"    destination:\n" +
		"      url: https://hooks.example/x\n" +
		"      signing:\n" +
		secrets
}

// mappingShapedSecrets writes the sensitive value as a sequence item the parser reads as a mapping,
// which is what an author does by writing a passphrase holding `: ` without quoting it.
const mappingShapedSecrets = "        secrets:\n" +
	"          - " + leakSentinel + "MAPPING-KEY: " + leakSentinel + "MAPPING-VALUE\n"

func TestAMappingShapedSensitiveValueIsRedactedFromItsFirstKey(t *testing.T) {
	document := wellFormedThrough(mappingShapedSecrets)

	_, warnings, errs := Parse([]byte(document), "listeners.yaml", MapEnv(nil))
	if len(errs) == 0 {
		t.Fatalf("the configuration produced no diagnostic, so nothing is rendered to search:\n%s",
			document)
	}

	rendered := errs.Render([]byte(document)) + warnings.Render([]byte(document))
	if strings.Contains(rendered, leakSentinel) {
		t.Errorf("a sensitive value the parser read as a mapping was blanked from its colon rather "+
			"than from its first key:\n%s", rendered)
	}
}

// mergeKeyStrippedSecrets is the same class reached by a third route, which the node-shape family
// found within the round's own fuzzing session: the value's own bytes end in `<<`, this library reads
// that as a merge key, and stage E refuses the merge and removes the entry -- leaving a mapping whose
// only remaining token is the colon and whose first key no longer exists to redact from.
const mergeKeyStrippedSecrets = "        secrets:\n" +
	"          - " + leakSentinel + "MERGE-HEAD<<: " + leakSentinel + "MERGE-TAIL\n"

// TestGoccyReadsATrailingMergeKeyIndicatorAsAMergeKey pins the library behaviour the case above
// rests on: `PREFIX<<:` is the merge key `<<`, and the text written before the indicator is no part
// of the key at all.
func TestGoccyReadsATrailingMergeKeyIndicatorAsAMergeKey(t *testing.T) {
	_, node := nodeAt(t, "list:\n  - PREFIX<<: tail\n", "$.list[0]")

	mapping, isMapping := node.(*ast.MappingNode)
	if !isMapping {
		t.Fatalf("$.list[0] is a %T, want a mapping", node)
	}
	if len(mapping.Values) != 1 {
		t.Fatalf("the mapping holds %d entries, want the one merge key", len(mapping.Values))
	}

	// Asked through the predicate stage E asks it through, so the pin measures what that stage sees.
	key := mapping.Values[0].Key
	if !key.IsMergeKey() {
		t.Errorf("the entry's key %q is not read as a merge key, so stage E would not remove it",
			key.String())
	}
	if strings.Contains(key.String(), "PREFIX") {
		t.Errorf("the entry's key is %q, which still holds the text written before the indicator; "+
			"the mapping would then keep a key of its own", key.String())
	}
}

// TestASensitiveMappingStrippedOfItsOwnKeysIsRedactedFromItsLine is why writtenStartOf answers a
// block mapping from its line rather than from its first key: a mapping need not have a key of its
// own by the time redaction reads it, and redacting from one that does not exist puts the answer back
// on the colon for exactly the documents whose secret is the text before it.
func TestASensitiveMappingStrippedOfItsOwnKeysIsRedactedFromItsLine(t *testing.T) {
	document := wellFormedThrough(mergeKeyStrippedSecrets)

	_, warnings, errs := Parse([]byte(document), "listeners.yaml", MapEnv(nil))
	if len(errs) == 0 {
		t.Fatalf("the configuration produced no diagnostic, so nothing is rendered to search:\n%s",
			document)
	}

	rendered := errs.Render([]byte(document)) + warnings.Render([]byte(document))
	if strings.Contains(rendered, leakSentinel) {
		t.Errorf("a sensitive mapping left without a key of its own was rendered from its colon:\n%s",
			rendered)
	}
}

// TestBothBranchesAnswerAMappingShapedSensitiveValue is the control that made the finding a defect
// rather than a policy: the key-scoped fallback blanks these very bytes, and two branches
// disagreeing about one input class is the disagreement D3 forbids. It fails if either branch
// stops answering, so a repair that fixed one of them alone would not satisfy it.
func TestBothBranchesAnswerAMappingShapedSensitiveValue(t *testing.T) {
	for _, tc := range []struct {
		name      string
		document  string
		pathAware bool
	}{
		{name: "path-aware", document: wellFormedThrough(mappingShapedSecrets), pathAware: true},
		{name: "key-scoped fallback", pathAware: false,
			document: wellFormedThrough(mappingShapedSecrets) + unparseableTail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := newSource("listeners.yaml", []byte(tc.document))
			if got := pathsAreResolvable(parseDocument(text)); got != tc.pathAware {
				t.Fatalf("the document takes the other branch (resolvable = %t), so it cannot show "+
					"what %s does:\n%s", got, tc.name, tc.document)
			}
			if rendered := renderEveryLineOf(tc.document); strings.Contains(rendered, leakSentinel) {
				t.Errorf("the %s branch quoted a mapping-shaped sensitive value:\n%s",
					tc.name, rendered)
			}
		})
	}
}

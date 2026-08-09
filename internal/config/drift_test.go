package config

import (
	"reflect"
	"slices"
	"testing"
)

// Whether the two places a key exists agree: schema.go declares which keys the contract has, and
// raw.go's `yaml` tags decide which of them a document can actually put a value into.
//
// **Both directions matter, and they fail differently.** A key declared in schema.go with no tag in
// raw.go is *checkable but not decodable*: the shape pass accepts it, no rule ever sees a value, and the
// line an author wrote is silently ignored. A tag with no declaration is *decodable but unchecked*: the
// value reaches the program while the shape pass reports the key as unknown, so the file cannot load at
// all. One-directional drift tests leave whichever half they do not test undetected, which is why this
// one enumerates the disagreement in both directions and names the key or tag that is unmatched rather
// than only failing.

// levelTypes is the raw struct each declared mapping level decodes into.
//
// It is a map of levels to types rather than a list of names, so it declares no key: the names it
// reconciles come from schema.go on one side and from the struct tags on the other, and this table
// carries neither (schemasource_test.go is what enforces that).
var levelTypes = map[levelName]reflect.Type{
	levelRoot:        reflect.TypeFor[rawConfig](),
	levelDatabase:    reflect.TypeFor[rawDatabase](),
	levelWorker:      reflect.TypeFor[rawWorker](),
	levelRetention:   reflect.TypeFor[rawRetention](),
	levelDefaults:    reflect.TypeFor[rawDefaults](),
	levelRetry:       reflect.TypeFor[rawRetry](),
	levelListener:    reflect.TypeFor[rawListener](),
	levelOperation:   reflect.TypeFor[rawOperation](),
	levelPayload:     reflect.TypeFor[rawPayload](),
	levelDestination: reflect.TypeFor[rawDestination](),
	levelSigning:     reflect.TypeFor[rawSigning](),
}

// levelsWithoutAStruct is the two levels no struct decodes, each for a reason of its own rather than by
// omission. Both are exempt from the reconciliation and neither is exempt from being listed here, so a
// third level cannot join them silently (.claude/rules/test-both-sides-of-an-exclusion-guard.md).
var levelsWithoutAStruct = map[levelName]string{
	// A header mapping declares no key names at all -- an author may send any HTTP field -- so there is
	// nothing for a tag to agree with.
	levelHeaders: "free-form: the contract declares no key names here",
	// The operations mapping's names *are* the contract's vocabulary, and rawOperations reads them from
	// the same table this test would compare against, so the two cannot disagree: there is one
	// declaration, not two (TestTheCanonicalOrderIsTheContractsOwn).
	levelOperations: "decoded from the table itself rather than from struct tags",
}

// TestEveryDeclaredKeyIsDecodable is the first direction: a key the contract declares with no `yaml` tag
// to receive it. Its cost is silence -- the author's line is accepted and then ignored -- which is the
// worse of the two failures.
func TestEveryDeclaredKeyIsDecodable(t *testing.T) {
	for level, declared := range decodedLevels(t) {
		t.Run(string(level), func(t *testing.T) {
			tagged := yamlTagsOf(levelTypes[level])

			// Through namesMissingFrom rather than an inline slices.Contains, so that
			// TestTheDriftTestFailsInBothDirections exercises the comparison this loop actually makes.
			// While the two were separate, that test proved a helper nothing else called and this loop was
			// free to become a self-comparison with the suite green
			// (.claude/rules/falsify-the-assertion-not-a-copy-of-it.md).
			for _, name := range namesMissingFrom(declared.declaredNames(), tagged) {
				t.Errorf("schema.go declares %q at level %q and raw.go has no yaml tag for it, so a "+
					"document writing it is checked and then discarded; raw.go tags %v", name, level, tagged)
			}
		})
	}
}

// TestEveryDecodableKeyIsDeclared is the other direction: a tag with no declaration behind it. Its cost
// is that the shape pass reports the key as unknown, so a file exercising it cannot load -- loud, but
// only for whoever writes that key.
func TestEveryDecodableKeyIsDeclared(t *testing.T) {
	for level, declared := range decodedLevels(t) {
		t.Run(string(level), func(t *testing.T) {
			names := declared.declaredNames()

			// The same helper with the arguments swapped, which is all the two directions differ by.
			for _, tag := range namesMissingFrom(yamlTagsOf(levelTypes[level]), names) {
				t.Errorf("raw.go tags %q at level %q and schema.go declares no such key, so the shape "+
					"pass would report it as unknown; schema.go declares %v", tag, level, names)
			}
		})
	}
}

// TestTheDriftTestCoversEveryDeclaredLevel keeps both directions from passing vacuously, and is what
// makes the two exemptions above a decision rather than a gap: every level of the contract is either
// reconciled against a struct or listed as having none, with the reason it has none.
func TestTheDriftTestCoversEveryDeclaredLevel(t *testing.T) {
	for level := range schemaLevels {
		_, reconciled := levelTypes[level]
		_, exempt := levelsWithoutAStruct[level]

		switch {
		case reconciled && exempt:
			t.Errorf("level %q is both reconciled and exempt", level)
		case !reconciled && !exempt:
			t.Errorf("level %q is neither reconciled against a raw struct nor listed as having none", level)
		}
	}
	for level := range levelTypes {
		if _, declared := schemaLevels[level]; !declared {
			t.Errorf("level %q has a raw struct and is not declared by the contract", level)
		}
	}
}

// TestAFreeFormLevelDeclaresNoKeyToReconcile is the exemption's own claim, asserted rather than assumed:
// a level listed as having no struct because it declares no keys must in fact declare none, or the
// exemption would be hiding real drift.
func TestAFreeFormLevelDeclaresNoKeyToReconcile(t *testing.T) {
	if names := schemaLevels[levelHeaders].declaredNames(); len(names) != 0 {
		t.Errorf("the header level declares %v; it is exempt from the drift test for declaring none", names)
	}
	if !schemaLevels[levelHeaders].freeForm {
		t.Error("the header level is no longer free-form, so it needs a struct and a reconciliation")
	}
}

// TestTheDriftTestFailsInBothDirections is the reconciliation's own falsifiability, which no run over an
// agreeing pair can demonstrate: it is applied to a deliberately disagreeing pair, once each way, and must
// report the key or tag that is unmatched.
//
// Without this, a reconciliation that compared a list with itself -- or that iterated nothing -- would pass
// every run and detect no drift at all. It exercises namesMissingFrom because that is the function both
// directions above now call; while they used an inline slices.Contains this test proved a helper nothing
// else reached (.claude/rules/falsify-the-assertion-not-a-copy-of-it.md).
func TestTheDriftTestFailsInBothDirections(t *testing.T) {
	declared := []string{"url", "schema", "listen_url"}

	t.Run("a key with no tag", func(t *testing.T) {
		tagged := []string{"url", "schema"}

		if missing := namesMissingFrom(declared, tagged); !slices.Equal(missing, []string{"listen_url"}) {
			t.Errorf("the reconciliation found %v, want the declared key no tag receives", missing)
		}
	})

	t.Run("a tag with no key", func(t *testing.T) {
		tagged := []string{"url", "schema", "listen_url", "listen_urls"}

		if extra := namesMissingFrom(tagged, declared); !slices.Equal(extra, []string{"listen_urls"}) {
			t.Errorf("the reconciliation found %v, want the tag no declaration backs", extra)
		}
	})
}

// namesMissingFrom is the members of one list the other lacks, which is the comparison both directions
// above make. It is one function because the two directions differ only in which list is which, and two
// copies could disagree about what "missing" means
// (.claude/rules/reuse-the-helper-before-copying-it.md).
func namesMissingFrom(these, those []string) []string {
	var missing []string
	for _, name := range these {
		if !slices.Contains(those, name) {
			missing = append(missing, name)
		}
	}
	return missing
}

// decodedLevels is the levels a struct decodes, which is what both directions iterate.
func decodedLevels(t *testing.T) map[levelName]mappingLevel {
	t.Helper()

	reconciled := make(map[levelName]mappingLevel, len(levelTypes))
	for level := range levelTypes {
		declared, isDeclared := schemaLevels[level]
		if !isDeclared {
			t.Fatalf("level %q has a raw struct and no declaration", level)
		}
		reconciled[level] = declared
	}
	if len(reconciled) == 0 {
		t.Fatal("no level is reconciled, so the drift assertions would pass vacuously")
	}
	return reconciled
}

// yamlTagsOf is the key each exported field of a raw struct decodes from, in declaration order.
//
// A field with no tag would decode from its Go name, which is not a key the contract can declare, so an
// untagged field is reported as the empty name and fails the reconciliation rather than passing it.
func yamlTagsOf(held reflect.Type) []string {
	tags := make([]string, 0, held.NumField())
	for i := range held.NumField() {
		field := held.Field(i)
		if !field.IsExported() {
			continue
		}
		tags = append(tags, field.Tag.Get("yaml"))
	}
	return tags
}

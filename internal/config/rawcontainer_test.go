package config

import (
	"testing"
)

// The two container carriers: that they read past the node properties an author may write, that they
// refuse a shape they cannot fill rather than returning it, and -- the claim the whole stage rests on --
// that no wrapper in the package has an exit that can answer non-nil.

// TestAContainerReadsPastTheNodePropertiesAnAuthorMayWrite is the measured reason these two types exist.
// A tagged mapping is legal YAML that stage F accepts, and the decoder refuses it outright where a struct
// is expected -- so without the peel a document the whole pipeline calls clean would decode to nothing.
//
// Both halves are asserted: the tagged spelling decodes, and it decodes to the *same* value the plain
// spelling does, so a peel that dropped the value would fail as surely as no peel at all.
func TestAContainerReadsPastTheNodePropertiesAnAuthorMayWrite(t *testing.T) {
	spellings := map[string]string{
		"a plain mapping":            "database:\n  url: postgres://noty:pw@db/noty\n",
		"a tagged mapping":           "database: !!map {url: postgres://noty:pw@db/noty}\n",
		"an anchored mapping":        "database: &db {url: postgres://noty:pw@db/noty}\n",
		"an anchored tagged mapping": "database: &db !!map {url: postgres://noty:pw@db/noty}\n",
	}

	for spelling, written := range spellings {
		t.Run(spelling, func(t *testing.T) {
			decoded := decodedConfig(t, "version: 1\n"+written+"listeners: []\n")

			if got := decoded.Database.Value.URL; got.value != "postgres://noty:pw@db/noty" {
				t.Errorf("database.url decoded to %q, want the value beneath the property", got.value)
			}
		})
	}
}

// TestAListReadsPastThePropertiesOnItselfAndOnItsElements is the same claim for the list carrier, whose
// elements may each carry properties of their own -- the arm the walk's own peel exists for.
func TestAListReadsPastThePropertiesOnItselfAndOnItsElements(t *testing.T) {
	listener := "    name: order_paid\n    table: public.orders\n    operations: [insert]\n" +
		"    destination:\n      url: https://hooks.example.test/order-paid\n"
	spellings := map[string]string{
		"a plain list":             "listeners:\n  - " + listener[4:],
		"a tagged list":            "listeners: !!seq\n  - " + listener[4:],
		"a tagged list element":    "listeners:\n  - !!map\n" + listener,
		"an anchored list element": "listeners:\n  - &first\n" + listener,
	}

	for spelling, written := range spellings {
		t.Run(spelling, func(t *testing.T) {
			decoded := decodedConfig(t, "version: 1\ndatabase:\n  url: postgres://noty:pw@db/noty\n"+written)

			if len(decoded.Listeners.Values) != 1 {
				t.Fatalf("decoded %d listeners, want 1", len(decoded.Listeners.Values))
			}
			if got := decoded.Listeners.Values[0].Name.value; got != "order_paid" {
				t.Errorf("the listener's name decoded to %q, want the value beneath the property", got)
			}
		})
	}
}

// TestAContainerRefusesAShapeItCannotFillRatherThanFailingTheDecode reaches the two refusals no document
// does. Stage F refuses a value whose YAML shape is not the one the contract declares and stops the run,
// so these branches are unreachable through the pipeline -- and a fail-closed branch nothing asserts is
// one that can be deleted with the suite still green.
//
// They are what makes "decode cannot fail" this stage's property rather than stage F's: were the refusal
// a returned error, removing stage F's stop would turn a wrong shape into an abandoned document.
func TestAContainerRefusesAShapeItCannotFillRatherThanFailingTheDecode(t *testing.T) {
	t.Run("a mapping that is not one", func(t *testing.T) {
		src, node := nodeAt(t, "database: text\n", "$.database")

		var held mappingOf[rawDatabase]
		if err := held.UnmarshalYAML(node); err != nil {
			t.Fatalf("UnmarshalYAML returned %v; a container must not fail the decode", err)
		}
		assertOneRefusal(t, src, &held, mustBeAMapping)
	})

	t.Run("a list that is not one", func(t *testing.T) {
		src, node := nodeAt(t, "listeners: text\n", "$.listeners")

		var held listOf[rawListener]
		if err := held.UnmarshalYAML(node); err != nil {
			t.Fatalf("UnmarshalYAML returned %v; a container must not fail the decode", err)
		}
		assertOneRefusal(t, src, &held, mustBeAList)
	})

	t.Run("a list element that is not a mapping", func(t *testing.T) {
		src, node := nodeAt(t, "listeners:\n  - text\n", "$.listeners")

		var held listOf[rawListener]
		if err := held.UnmarshalYAML(node); err != nil {
			t.Fatalf("UnmarshalYAML returned %v; a container must not fail the decode", err)
		}
		diags := assertOneRefusal(t, src, &held, mustBeAnEntryOf)
		// `  - ` is four runes, so the element begins at rune 5 of line 2: the refusal anchors on the
		// element rather than on the list, which is where the author has to look.
		if diags[0].Line != 2 || diags[0].Col != 5 {
			t.Errorf("recorded at %d:%d, want the element's own 2:5", diags[0].Line, diags[0].Col)
		}
	})
}

// assertOneRefusal resolves a container and asserts it recorded exactly the given fault, returning the
// diagnostics so a case can add a claim about where they point.
func assertOneRefusal(t *testing.T, src *source, held resolvable, want fault) Errors {
	t.Helper()

	pass := newDecodePass(src)
	held.resolve(pass)

	if len(pass.diags) != 1 {
		t.Fatalf("recorded %q, want exactly one refusal", messagesOf(pass.diags))
	}
	if pass.diags[0].Msg != want.message {
		t.Errorf("Msg = %q, want %q", pass.diags[0].Msg, want.message)
	}
	return pass.diags
}

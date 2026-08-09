package reconcile

import "testing"

const actionKindCount = 5

func TestActionKindsAreClosedAndClassifiedPerKind(t *testing.T) {
	// Range the declared vocabulary and pin its contract size instead of asserting a copied
	// list.
	if got := len(actionKinds); got != actionKindCount {
		t.Fatalf("actionKinds declares %d values, want %d", got, actionKindCount)
	}

	wantDestructive := map[ActionKind]bool{
		"create":  false,
		"replace": false,
		"drop":    true,
		"rename":  false,
		"disable": true,
	}
	seen := map[ActionKind]bool{}
	for _, kind := range actionKinds {
		t.Run(string(kind), func(t *testing.T) {
			if kind == "" {
				t.Fatal("the zero ActionKind must not be a declared action")
			}
			if seen[kind] {
				t.Fatalf("action kind %q appears twice", kind)
			}
			seen[kind] = true
			want, known := wantDestructive[kind]
			if !known {
				t.Fatalf("undeclared action kind %q appeared in the closed set", kind)
			}
			if got := kind.Destructive(); got != want {
				t.Errorf("%q.Destructive() = %t, want %t", kind, got, want)
			}
		})
	}
	if got := len(seen); got != len(wantDestructive) {
		t.Fatalf("declared action set has %d members, want %d", got, len(wantDestructive))
	}
}

func TestAnUnclassifiedActionFailsClosedAsDestructive(t *testing.T) {
	// actionKinds enumerates every kind a valid plan can carry today. Constructing a value outside that set
	// reaches the default arm directly.
	const unclassified ActionKind = "future_action"
	if unclassified.Destructive() != true {
		t.Fatal("an unclassified action must be destructive: otherwise it could be dropped without permission")
	}
}

func TestActionSymbolsAreThePinnedLiterals(t *testing.T) {
	// The rendered symbols are mandated, so pin their literals instead of deriving them from
	// Symbol.
	wantSymbols := map[ActionKind]string{
		ActionCreate:  "+",
		ActionReplace: "-/+",
		ActionDrop:    "-",
		ActionRename:  "~",
		ActionDisable: "-",
	}
	for _, kind := range actionKinds {
		if got, want := kind.Symbol(), wantSymbols[kind]; got != want {
			t.Errorf("%q.Symbol() = %q, want specification literal %q", kind, got, want)
		}
	}
}

func TestReplaceNeverRendersTheRenameOnlySymbol(t *testing.T) {
	const renameOnlySymbol = "~"
	if got := ActionReplace.Symbol(); got == renameOnlySymbol {
		t.Fatalf("replace rendered %q; only rename may render the in-place symbol", got)
	}
	for _, kind := range actionKinds {
		got := kind.Symbol()
		if kind == ActionRename && got != renameOnlySymbol {
			t.Fatalf("rename rendered %q, want %q", got, renameOnlySymbol)
		}
		if kind != ActionRename && got == renameOnlySymbol {
			t.Fatalf("%q rendered %q; the symbol is rename-only", kind, got)
		}
	}
}

package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/cpouldev/pg_noty/internal/reconcile"
)

func TestApprovalGridCrossesOrthogonalFlags(t *testing.T) {
	axes := [2][]bool{{false, true}, {false, true}}
	if len(axes[0])*len(axes[1]) != 4 {
		t.Fatal("approval axes are not a four-cell grid")
	}
	plan := reconcile.PlanResult{Actions: []reconcile.Action{{Kind: reconcile.ActionDrop}}}
	for _, allow := range axes[0] {
		for _, auto := range axes[1] {
			approval := approvalFor(&rootOptions{AllowDelete: allow, AutoApprove: auto}, false)
			want := "approval"
			if !allow {
				want = "destruction permission"
			} else if auto {
				want = "proceed"
			}
			if got := approvalReason(plan, approval); got != want {
				t.Fatalf("allow=%t auto=%t reason=%q, want %q", allow, auto, got, want)
			}
		}
	}
}

func TestApprovalClassifiesReplaceDropDisableAndClean(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind reconcile.ActionKind
		want string
	}{
		{"replace", reconcile.ActionReplace, "proceed"},
		{"drop", reconcile.ActionDrop, "destruction permission"},
		{"disable", reconcile.ActionDisable, "destruction permission"},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				plan := reconcile.PlanResult{Actions: []reconcile.Action{{Kind: tc.kind}}}
				if got := approvalReason(plan, approvalFor(&rootOptions{AutoApprove: true}, false)); got != tc.want {
					t.Fatalf("reason = %q, want %q", got, tc.want)
				}
			},
		)
	}
	clean := reconcile.PlanResult{}
	approval := approvalFor(&rootOptions{}, false)
	if approval.DestructionPermitted || approval.Approved || approval.Interactive {
		t.Fatalf("clean approval = %+v", approval)
	}
	if clean.Destructive() || len(clean.Actions) != 0 {
		t.Fatalf("clean plan unexpectedly changed: %+v", clean)
	}
}

func TestInteractiveFromInfoUsesBothSidesOfTheGuard(t *testing.T) {
	if interactiveFromInfo(nil) {
		t.Fatal("nil file info reported interactive")
	}
}

func TestConfirmVocabularyAndEOF(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  bool
	}{
		{"affirmative", "y\n", true},
		{"mixed affirmative", "YeS\n", true},
		{"negative", "n\n", false},
		{"empty", "\n", false},
		{"unrelated", "later\n", false},
		{"end of input", "", false},
	} {
		t.Run(
			tc.name, func(t *testing.T) {
				if got := confirm(context.Background(), strings.NewReader(tc.input), ioDiscard{}); got != tc.want {
					t.Fatalf("confirm(%q) = %t, want %t", tc.input, got, tc.want)
				}
			},
		)
	}
}

package config

import "testing"

// AC #6 as one decision with two askers. keyreference.go answers "may this key stand", stage D
// asks it of every key an author wrote, and stage E asks it of every key the operations fold
// invents. What each stage then does with the answer is its own file's subject; that they get the
// same answer is this file's.

// keyTextsBothStagesJudge is one key text per verdict the decision gives: refused for holding the
// delimiter, refused for holding it as the escape -- a key is never interpolated, so `$${` is
// never unescaped inside one either -- and cleared.
var keyTextsBothStagesJudge = map[string]string{
	"a key holding a reference": "${SECRET}",
	"a key holding the escape":  "$${LITERAL}",
	"a key holding neither":     "insert",
}

// TestBothStagesThatMeetAKeyGiveItOneVerdict is what keeps AC #6 a single invariant while two
// stages enforce it.
//
// It drives the two stages rather than the shared function, so it falsifies the thing that can
// actually go wrong: a condition added to one stage's key handling and not the other's. The
// stages are compared on the message, because that is what the author reads -- a reader must not
// be able to tell which stage refused a key, since it is one condition and one remedy.
func TestBothStagesThatMeetAKeyGiveItOneVerdict(t *testing.T) {
	for name, text := range keyTextsBothStagesJudge {
		t.Run(name, func(t *testing.T) {
			written := refusalOfAKeyTheAuthorWrote(t, text)
			introduced := refusalOfAKeyTheFoldIntroduces(t, text)

			if written != introduced {
				t.Errorf("the key %q is answered %q where an author writes it and %q where the fold "+
					"introduces it; AC #6 is one decision", text, written, introduced)
			}
		})
	}
}

// refusalOfAKeyTheAuthorWrote is what stage D says about text written as a mapping key, and is
// empty when it says nothing.
func refusalOfAKeyTheAuthorWrote(t *testing.T, text string) string {
	t.Helper()

	_, _, _, faults := stageD(t, "\""+text+"\": v\n", nil)
	return onlyMessageOf(t, faults)
}

// refusalOfAKeyTheFoldIntroduces is what stage E says about the same text arriving as an
// operations list element, and is empty when it says nothing.
//
// The text reaches the element from the environment rather than from the document, because that
// is the only route by which bytes holding `${` can arrive in a key position at all: stage D
// substituted into the element while it was a value, and what it wrote there is opaque by design
// (ADR-5), so nothing scanned it as a key before the fold made it one.
func refusalOfAKeyTheFoldIntroduces(t *testing.T, text string) string {
	t.Helper()

	// A block list, because `[${OPERATION}]` is a flow sequence whose `{` opens a flow mapping.
	_, _, diags := stageE(t, "operations:\n  - ${OPERATION}\n", map[string]string{"OPERATION": text})
	return onlyMessageOf(t, diags)
}

// onlyMessageOf is the message of the one diagnostic a stage raised, or the empty string when it
// raised none. More than one is a failure rather than a choice: a key is one condition, so a
// second diagnostic would mean the comparison above was reading whichever came first.
func onlyMessageOf(t *testing.T, diags Errors) string {
	t.Helper()

	if len(diags) > 1 {
		t.Fatalf("a stage raised %d diagnostics %q about one key, want at most one",
			len(diags), messagesOf(diags))
	}
	if len(diags) == 0 {
		return ""
	}
	return diags[0].Msg
}

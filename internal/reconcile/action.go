package reconcile

// ActionKind is one possible change a reconciliation plan can contain.
type ActionKind string

const (
	ActionCreate  ActionKind = "create"
	ActionReplace ActionKind = "replace"
	ActionDrop    ActionKind = "drop"
	ActionRename  ActionKind = "rename"
	ActionDisable ActionKind = "disable"
)

// actionKinds is the closed vocabulary valid plans can contain, pinned at five members by action_test.go.
var actionKinds = []ActionKind{
	ActionCreate,
	ActionReplace,
	ActionDrop,
	ActionRename,
	ActionDisable,
}

// Destructive reports whether an action requires destruction permission. Replace is deliberately
// non-destructive: it is the ordinary specification-change path, so gating it makes permission routine.
// An unrecognised action fails closed to prevent it being dropped without permission. That safe
// classification is unconditional: no property of an unrecognised value may lower the guard; see
// TestAnUnclassifiedActionFailsClosedAsDestructive.
func (kind ActionKind) Destructive() bool {
	switch kind {
	case ActionCreate, ActionReplace, ActionRename:
		return false
	default:
		return true
	}
}

// Symbol returns Terraform-shaped plan notation. Rename is the one in-place update.
func (kind ActionKind) Symbol() string {
	switch kind {
	case ActionCreate:
		return "+"
	case ActionReplace:
		return "-/+"
	case ActionDrop, ActionDisable:
		return "-"
	case ActionRename:
		return "~"
	default:
		return ""
	}
}

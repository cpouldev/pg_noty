package reconcile

import "strings"

// Render writes a Terraform-shaped plan grouped by listener and then operation. Its order comes from
// orderedActions, never the input collection, and ActionKind.Symbol is this package's single symbol
// authority.
func (result PlanResult) Render() string {
	var rendered strings.Builder
	var listener, operation string
	for _, action := range orderedActions(result.Actions) {
		if action.Pair.Listener != listener {
			listener = action.Pair.Listener
			operation = ""
			rendered.WriteString("listener ")
			rendered.WriteString(listener)
			rendered.WriteString(":\n")
		}
		if action.Pair.Operation != operation {
			operation = action.Pair.Operation
			rendered.WriteString("  operation ")
			rendered.WriteString(operation)
			rendered.WriteString(":\n")
		}
		rendered.WriteString("    ")
		rendered.WriteString(action.Kind.Symbol())
		rendered.WriteString(" ")
		rendered.WriteString(string(action.Kind))
		rendered.WriteString("\n")
	}
	return rendered.String()
}

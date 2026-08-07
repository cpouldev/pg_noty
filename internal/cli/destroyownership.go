package cli

import (
	"fmt"

	"github.com/cpouldev/pg_noty/internal/reconcile"
)

// The two axes are deliberately closed: every ownership disagreement has a message for each
// catalog object kind. Keeping the product here makes adding a disagreement or kind observable.
var destroyObjectKinds = []string{"trigger", "function"}

var destroyDisagreementMessages = map[reconcile.Disagreement]map[string]string{
	reconcile.DisagreementMarkerAbsent: {
		"trigger":  "trigger %s has no ownership evidence (%s); leave it in place and confirm its owner",
		"function": "function %s has no ownership evidence (%s); leave it in place and confirm its owner",
	},
	reconcile.DisagreementForeignInstance: {
		"trigger":  "trigger %s belongs to another instance (%s); remove it only with that instance",
		"function": "function %s belongs to another instance (%s); remove it only with that instance",
	},
	reconcile.DisagreementAnotherPair: {
		"trigger":  "trigger %s is recorded for another listener pair (%s); repair the registry first",
		"function": "function %s is recorded for another listener pair (%s); repair the registry first",
	},
	reconcile.DisagreementNoRegistryRow: {
		"trigger":  "trigger %s has no registry row (%s); restore the record before removing it",
		"function": "function %s has no registry row (%s); restore the record before removing it",
	},
}

func destroyMessage(kind string, disagreement reconcile.Disagreement, identity, evidence string) string {
	if cells, ok := destroyDisagreementMessages[disagreement]; ok {
		if format, ok := cells[kind]; ok {
			return fmt.Sprintf(format, identity, evidence)
		}
	}
	return fmt.Sprintf("%s %s is not proven owned (%s); leave it in place", kind, identity, evidence)
}

func destroyCellCount() int { return len(destroyObjectKinds) * len(destroyDisagreementMessages) }

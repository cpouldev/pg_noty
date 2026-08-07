package reconcile

import (
	"github.com/cpouldev/pg_noty/internal/config"
)

// Deferred validation has four identities, deliberately outside config's R1-R42
// and its seven structural rule names. rules_test.go reconciles this size-pinned
// set against config.DeferredKind with go/ast so neither side can quietly drift.
var deferredRules = map[config.DeferredKind]config.RuleID{
	config.TableExists:       "D1",
	config.ColumnsExist:      "D2",
	config.PrimaryKeyPresent: "D3",
	config.WhenParses:        "D4",
}

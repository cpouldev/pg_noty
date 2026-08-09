package reconcile

import "github.com/cpouldev/pg_noty/internal/config"

// positionedCheck is the only DeferredCheck-to-Positioned adapter. DeferredCheck's File, Line,
// Col and Path fields mean methods of those names on DeferredCheck itself do not compile; this
// wrapper preserves the frozen config port without renaming its fields.
type positionedCheck struct{ check config.DeferredCheck }

func (positioned positionedCheck) File() string { return positioned.check.File }

func (positioned positionedCheck) Line() int { return positioned.check.Line }

func (positioned positionedCheck) Col() int { return positioned.check.Col }

func (positioned positionedCheck) Path() string { return positioned.check.Path }

package reconcile

// Verdict describes whether a reconciliation plan is clean, needs changes, or cannot proceed.
type Verdict string

const (
	VerdictClean          Verdict = "clean"
	VerdictChangesPending Verdict = "changes_pending"
	VerdictError          Verdict = "error"
)

// verdicts is the closed vocabulary pinned at three members by verdict_test.go.
var verdicts = []Verdict{
	VerdictClean,
	VerdictChangesPending,
	VerdictError,
}

// ExitCode is the plan-result mapping internal/cli must quote rather than re-declare: clean 0,
// changes pending 2, error 1.
func (verdict Verdict) ExitCode() int {
	switch verdict {
	case VerdictClean:
		return 0
	case VerdictChangesPending:
		return 2
	default:
		return 1
	}
}

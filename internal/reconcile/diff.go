package reconcile

import (
	"cmp"
	"slices"
)

// desiredListener preserves configuration even when it deliberately has no operations.
type desiredListener struct {
	Name, SpecHash              string
	Enabled, SpecificationKnown bool
	ChangedOperations           map[string]bool
}

// diffInput supplies all three sources. It contains no port: callers perform their reads before it.
type diffInput struct {
	Desired   []desiredPair
	Recorded  []recordedPair
	Observed  []observedPair
	Listeners []desiredListener
	Targets   map[string]targetResolution
	Instance  string
}

// diff walks every pair named by desired configuration, registry, or catalog and returns its plan.
func diff(input diffInput) PlanResult {
	sources := collectPairSources(input)
	pairs := orderedPairs(sources)
	result := PlanResult{Examined: len(pairs)}
	for _, pair := range pairs {
		appendClassification(&result, classifyPair(preparedPairSource(input, sources, pair)))
	}
	return finaliseDiff(result)
}

// preparedPairSource is the one place a pair gains the run-wide facts classification needs. It lets the
// RetainListener test run the walk's preparation, not restate it (falsify-the-assertion-not-a-copy-of-it).
func preparedPairSource(input diffInput, sources map[Pair]pairSources, pair Pair) pairSources {
	source := sources[pair]
	source.Instance = input.Instance
	source.Target = input.Targets[pair.Listener]
	shortCircuitSpecificationComparison(&source)
	return source
}

func collectPairSources(input diffInput) map[Pair]pairSources {
	sources := make(map[Pair]pairSources)
	for _, desired := range input.Desired {
		source := sources[desired.Pair]
		source.Desired = desired
		sources[desired.Pair] = source
	}
	for _, recorded := range input.Recorded {
		source := sources[recorded.Pair]
		source.Recorded = recorded
		sources[recorded.Pair] = source
	}
	for _, observed := range input.Observed {
		source := sources[observed.Pair]
		source.Observed = observed
		sources[observed.Pair] = source
	}
	applyDesiredListeners(sources, input.Listeners)
	return sources
}

func applyDesiredListeners(sources map[Pair]pairSources, listeners []desiredListener) {
	byName := make(map[string]desiredListener, len(listeners))
	for _, listener := range listeners {
		byName[listener.Name] = listener
	}
	for pair, source := range sources {
		listener, found := byName[pair.Listener]
		if found {
			source.Desired.ListenerPresent = true
			source.Desired.Enabled = listener.Enabled
			source.Desired.SpecHash = listener.SpecHash
			source.Desired.SpecificationChanged, source.Desired.SpecificationKnown = listener.ChangedOperations[pair.Operation], listener.SpecificationKnown
			source.Desired.Pair = pair
		}
		sources[pair] = source
	}
}

func orderedPairs(sources map[Pair]pairSources) []Pair {
	pairs := make([]Pair, 0, len(sources))
	for pair := range sources {
		pairs = append(pairs, pair)
	}
	slices.SortFunc(pairs, func(left, right Pair) int {
		if byListener := cmp.Compare(left.Listener, right.Listener); byListener != 0 {
			return byListener
		}
		return cmp.Compare(left.Operation, right.Operation)
	})
	return pairs
}

// shortCircuitSpecificationComparison never bypasses catalog absence or drift (pin-documented-precedence).
func shortCircuitSpecificationComparison(source *pairSources) {
	if source.Desired.SpecificationKnown {
		return
	}
	source.Desired.SpecificationChanged = source.Recorded.ListenerPresent && source.Desired.SpecHash != source.Recorded.Listener.SpecHash
}

func appendClassification(result *PlanResult, classification pairClassification) {
	if classification.Refusal != nil {
		result.Refusals = append(result.Refusals, *classification.Refusal)
		return
	}
	if classification.Action != nil {
		result.Actions = append(result.Actions, *classification.Action)
	}
}

func finaliseDiff(result PlanResult) PlanResult {
	if len(result.Refusals) != 0 {
		result.Actions = nil
		result.Verdict = VerdictError
		return result
	}
	result.Actions = orderedActions(result.Actions)
	if len(result.Actions) == 0 {
		result.Verdict = VerdictClean
		return result
	}
	result.Verdict = VerdictChangesPending
	return result
}

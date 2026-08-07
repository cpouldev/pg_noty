package reconcile

// desiredPair is the configuration side of one pair supplied to the pure classifier.
type desiredPair struct {
	Pair                                              Pair
	ListenerPresent, OperationPresent                 bool
	Enabled, SpecificationChanged, SpecificationKnown bool
	SpecHash                                          string
}

// recordedPair is the registry side of one pair supplied to the pure classifier.
type recordedPair struct {
	Pair                            Pair
	ListenerPresent, TriggerPresent bool
	Listener                        registryListener
	Trigger                         registryTrigger
}

// observedPair is the catalog side of one pair supplied to the pure classifier.
type observedPair struct {
	Pair                            Pair
	TriggerPresent, FunctionPresent bool
	Trigger                         TriggerReading
	Function                        FunctionReading
}

type pairSources struct {
	Desired  desiredPair
	Recorded recordedPair
	Observed observedPair
	Target   targetResolution
	Instance string
}

type pairClassification struct {
	Action         *Action
	Refusal        *Refusal
	RetainListener bool
}

type matrixRow struct {
	Criterion int
	Name      string
	Assertion string
}

// matrixRows is the complete classification population, criteria 11-25. Its assertion names are
// the evidence matrixregistry_test.go reconciles, rather than a count that cannot name an omitted
// row.
var matrixRows = [...]matrixRow{
	{11, "new listener", "TestClassificationMatrix/criterion_11_new_listener"},
	{12, "matching listener", "TestClassificationMatrix/criterion_12_matching_listener"},
	{13, "changed specification", "TestClassificationMatrix/criterion_13_changed_specification"},
	{14, "added operation", "TestClassificationMatrix/criterion_14_added_operation"},
	{15, "removed operation", "TestClassificationMatrix/criterion_15_removed_operation"},
	{16, "no operations retained", "TestClassificationMatrix/criterion_16_no_operations_retained"},
	{17, "listener removed", "TestClassificationMatrix/criterion_17_listener_removed"},
	{18, "empty listener list", "TestClassificationMatrix/criterion_18_empty_listener_list"},
	{19, "disabled listener", "TestClassificationMatrix/criterion_19_disabled_listener"},
	{20, "stably disabled listener", "TestClassificationMatrix/criterion_20_stably_disabled_listener"},
	{21, "missing catalog trigger", "TestClassificationMatrix/criterion_21_missing_catalog_trigger"},
	{22, "wrong catalog definition", "TestClassificationMatrix/criterion_22_wrong_catalog_definition"},
	{23, "dropped target", "TestClassificationMatrix/criterion_23_dropped_target"},
	{24, "renamed target", "TestClassificationMatrix/criterion_24_renamed_target"},
	{25, "dropped and recreated target", "TestClassificationMatrix/criterion_25_dropped_and_recreated_target"},
}

// classifyPair is the sole L2 seam from catalog and registry readings into the L1 ownership values.
// Routing every caller through it keeps ownership.go total over L1 values rather than growing a
// second copy of the same decision.
func classifyPair(source pairSources) pairClassification {
	if source.Target.Refusal != nil {
		return pairClassification{Refusal: source.Target.Refusal}
	}
	if refusal := observedOwnershipRefusal(source); refusal != nil {
		return pairClassification{Refusal: refusal}
	}
	if !source.Desired.ListenerPresent {
		return classifyRemovedListener(source)
	}
	if !source.Desired.OperationPresent {
		return classifyRemovedOperation(source)
	}
	if !source.Desired.Enabled {
		return classifyDisabled(source)
	}
	return classifyManagedPair(source)
}

func classifyRemovedListener(source pairSources) pairClassification {
	if !source.Recorded.TriggerPresent && !source.observedAny() {
		if source.Recorded.ListenerPresent {
			return classifiedAction(source.pair(), ActionDrop, false)
		}
		return pairClassification{}
	}
	return classifiedAction(source.pair(), ActionDrop, false)
}

func classifyRemovedOperation(source pairSources) pairClassification {
	if !source.Recorded.TriggerPresent && !source.observedAny() {
		return pairClassification{}
	}
	return classifiedAction(source.pair(), ActionDrop, true)
}

func classifyDisabled(source pairSources) pairClassification {
	if !source.Recorded.TriggerPresent && !source.observedAny() {
		return pairClassification{}
	}
	return classifiedAction(source.pair(), ActionDisable, true)
}

func classifyManagedPair(source pairSources) pairClassification {
	if source.Target.Outcome == targetDroppedAndRecreated {
		return classifiedAction(source.pair(), ActionReplace, true)
	}
	if !source.Recorded.TriggerPresent || !source.observedComplete() {
		return classifiedAction(source.pair(), ActionCreate, true)
	}
	if source.Target.Outcome == targetRenamed {
		return classifiedAction(source.pair(), ActionRename, true)
	}
	if source.Desired.SpecificationChanged || source.observedFingerprint() != source.Recorded.Trigger.DDLHash {
		return classifiedAction(source.pair(), ActionReplace, true)
	}
	return pairClassification{}
}

func observedOwnershipRefusal(source pairSources) *Refusal {
	row := registryOwnershipRow(source.Recorded)
	if source.Observed.TriggerPresent {
		object := catalogTriggerObject(source.Observed.Trigger, source.objectIdentity("trigger"))
		if !DetermineOwnership(row, object, source.Instance).Owned {
			refusal := ownershipRefusal(object.Identity, object.Marker)
			return &refusal
		}
	}
	if source.Observed.FunctionPresent {
		object := catalogFunctionObject(source.Observed.Function, source.objectIdentity("function"))
		if !DetermineOwnership(row, object, source.Instance).Owned {
			refusal := ownershipRefusal(object.Identity, object.Marker)
			return &refusal
		}
	}
	return nil
}

func registryOwnershipRow(recorded recordedPair) RegistryRow {
	return RegistryRow{Present: recorded.TriggerPresent, Listener: recorded.Pair.Listener, Operation: recorded.Pair.Operation}
}

func catalogTriggerObject(reading TriggerReading, identity string) CatalogObject {
	return CatalogObject{Kind: "trigger", Identity: identity, Marker: reading.Marker}
}

func catalogFunctionObject(reading FunctionReading, identity string) CatalogObject {
	return CatalogObject{Kind: "function", Identity: identity, Marker: reading.Marker}
}

func (source pairSources) pair() Pair {
	if source.Desired.Pair.Listener != "" {
		return source.Desired.Pair
	}
	if source.Recorded.Pair.Listener != "" {
		return source.Recorded.Pair
	}
	return source.Observed.Pair
}

func (source pairSources) objectIdentity(kind string) string {
	pair := source.pair()
	name := source.Recorded.Trigger.TriggerName
	if kind == "function" {
		name = source.Recorded.Trigger.FunctionName
	}
	if name == "" {
		name = pair.Listener + "/" + pair.Operation
	}
	return kind + " " + name
}

func (source pairSources) observedAny() bool {
	return source.Observed.TriggerPresent || source.Observed.FunctionPresent
}

func (source pairSources) observedComplete() bool {
	return source.Observed.TriggerPresent && source.Observed.FunctionPresent
}

func (source pairSources) observedFingerprint() string {
	return fingerprint(source.Observed.Trigger.Definition, source.Observed.Function.Definition)
}

func classifiedAction(pair Pair, kind ActionKind, retainListener bool) pairClassification {
	action := Action{Pair: pair, Kind: kind}
	return pairClassification{Action: &action, RetainListener: retainListener}
}

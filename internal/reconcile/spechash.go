package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/cpouldev/pg_noty/internal/config"
)

// listenerSpec is the one derived value written to listeners.spec and listeners.spec_hash.
type listenerSpec struct {
	Bytes []byte
	Hash  string
}

// deriveListenerSpec serialises only config.Listener.Trigger and derives its stored SHA-256 hex
// hash from those exact bytes. No field filtering is needed: TriggerSpec's three position fields
// are unexported and skipped by construction, and config.Operations is an ordered slice rather
// than a map precisely so no downstream specification hash depends on map iteration order (R9).
func deriveListenerSpec(trigger config.TriggerSpec) (listenerSpec, error) {
	bytes, err := json.Marshal(trigger)
	if err != nil {
		return listenerSpec{}, err
	}
	sum := sha256.Sum256(bytes)
	return listenerSpec{Bytes: bytes, Hash: hex.EncodeToString(sum[:])}, nil
}

// listenerOperationChanges compares the persisted trigger subtree with the desired one. A shared
// trigger field changes every operation; an operation's own fields change only its matching pair.
func listenerOperationChanges(prior registryListener, desired config.TriggerSpec) (map[string]bool, bool, error) {
	if prior.Name == "" {
		return nil, false, nil
	}
	var recorded config.TriggerSpec
	if err := json.Unmarshal(prior.Spec, &recorded); err != nil {
		return nil, false, err
	}
	changed, err := changedTriggerOperations(recorded, desired)
	return changed, true, err
}

func changedTriggerOperations(recorded, desired config.TriggerSpec) (map[string]bool, error) {
	shared, err := triggerSharedFieldsChanged(recorded, desired)
	if err != nil {
		return nil, err
	}
	prior, err := operationJSONByKind(recorded.Operations)
	if err != nil {
		return nil, err
	}
	current, err := operationJSONByKind(desired.Operations)
	if err != nil {
		return nil, err
	}
	changed := make(map[string]bool, len(current))
	for kind, encoded := range current {
		changed[kind] = shared || prior[kind] != encoded
	}
	return changed, nil
}

func triggerSharedFieldsChanged(recorded, desired config.TriggerSpec) (bool, error) {
	recorded.Operations, desired.Operations = nil, nil
	prior, err := deriveListenerSpec(recorded)
	if err != nil {
		return false, err
	}
	current, err := deriveListenerSpec(desired)
	return prior.Hash != current.Hash, err
}

func operationJSONByKind(operations config.Operations) (map[string]string, error) {
	encoded := make(map[string]string, len(operations))
	for _, operation := range operations {
		bytes, err := json.Marshal(operation)
		if err != nil {
			return nil, err
		}
		encoded[operation.Kind] = string(bytes)
	}
	return encoded, nil
}

func configuredByName(listeners []config.Listener) map[string]config.Listener {
	byName := make(map[string]config.Listener, len(listeners))
	for _, listener := range listeners {
		byName[listener.Name] = listener
	}
	return byName
}

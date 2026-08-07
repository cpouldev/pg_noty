package config

import "slices"

func listenerOperation(cfg Config, listener int, kind string) (Operation, bool) {
	if listener < 0 || listener >= len(cfg.Listeners) {
		return Operation{}, false
	}
	for _, operation := range cfg.Listeners[listener].Trigger.Operations {
		if operation.Kind == kind {
			return operation, true
		}
	}
	return Operation{}, false
}

func operationKindsEqual(listener Listener, want []string) bool {
	got := make([]string, len(listener.Trigger.Operations))
	for i, operation := range listener.Trigger.Operations {
		got[i] = operation.Kind
	}
	return slices.Equal(got, want)
}

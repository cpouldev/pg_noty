package source

import (
	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
)

// Generate is the pure compiler entry point. Its order is the workflow's contract: validate the
// whole input before deriving names or rendering text, then compose the existing leaf builders.
func Generate(request Request) ([]ObjectSet, error) {
	if err := validateGenerationInput(request); err != nil {
		return nil, err
	}
	operations := orderedOperations(request.Listener.Trigger.Operations)
	sets := make([]ObjectSet, 0, len(operations))
	for _, operation := range operations {
		abbreviation, _ := operationAbbreviationFor(operation.Kind)
		functionName := schema.ObjectName("pg_noty_"+request.Listener.Name, abbreviation)
		statements, err := statementsFor(request, operation, functionName)
		if err != nil {
			return nil, err
		}
		sets = append(
			sets, ObjectSet{
				Operation: operation.Kind, FunctionName: functionName, TriggerName: functionName,
				Marker:         marker(request.Instance, request.Listener.Name, operation.Kind),
				CreateFunction: statements.createFunction, RevokeExecute: statements.revokeExecute,
				CommentFunction: statements.commentFunction, CreateTrigger: statements.createTrigger,
				CommentTrigger: statements.commentTrigger,
			},
		)
	}
	return sets, nil
}

func validateGenerationInput(request Request) error {
	if err := validateRequestIdentifiers(request); err != nil {
		return err
	}
	payload := request.Listener.Trigger.Payload
	if err := payloadModeKnown(payload.Mode); err != nil {
		return err
	}
	for _, operation := range request.Listener.Trigger.Operations {
		if _, err := operationAbbreviationFor(operation.Kind); err != nil {
			return err
		}
		for _, column := range operation.Columns {
			if err := identifierRefusal("update column", column); err != nil {
				return err
			}
		}
	}
	for _, column := range append(append([]string{}, payload.Columns...), payload.Exclude...) {
		if err := identifierRefusal("payload column", column); err != nil {
			return err
		}
	}
	if payload.Mode == "keys_only" && len(request.Target.PrimaryKeyColumns) == 0 {
		return missingPrimaryKey(request.Listener.Name)
	}
	return nil
}

func orderedOperations(operations config.Operations) []config.Operation {
	ordered := make([]config.Operation, 0, len(operations))
	for _, known := range operationAbbreviations {
		for _, operation := range operations {
			if operation.Kind == known.kind {
				ordered = append(ordered, operation)
			}
		}
	}
	return ordered
}

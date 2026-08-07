package source

import (
	"strings"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
)

type triggerStatements struct {
	createFunction  string
	revokeExecute   string
	commentFunction string
	createTrigger   string
	commentTrigger  string
}

func statementsFor(request Request, operation config.Operation, functionName string) (triggerStatements, error) {
	if err := validateRequestIdentifiers(request); err != nil {
		return triggerStatements{}, err
	}
	if _, err := operationAbbreviationFor(operation.Kind); err != nil {
		return triggerStatements{}, err
	}
	if err := identifierRefusal("function", functionName); err != nil {
		return triggerStatements{}, err
	}
	function, reason := schema.Qualified(request.ServiceSchema, functionName)
	if reason != schema.IdentifierOK {
		return triggerStatements{}, unusableIdentifierError{field: "function", value: functionName, reason: reason}
	}
	target, reason := schema.Qualified(request.Target.Schema, request.Target.Table)
	if reason != schema.IdentifierOK {
		return triggerStatements{}, unusableIdentifierError{
			field: "target", value: request.Target.Table, reason: reason,
		}
	}
	body, err := triggerBody(request, operation)
	if err != nil {
		return triggerStatements{}, err
	}
	tag := dollarQuoteTag(body)
	markerText := quoteLiteral(marker(request.Instance, request.Listener.Name, operation.Kind))
	trigger, fault := schema.Quoted(functionName)
	if fault != schema.IdentifierOK {
		return triggerStatements{}, unusableIdentifierError{field: "trigger", value: functionName, reason: fault}
	}
	columns, err := updateColumns(operation)
	if err != nil {
		return triggerStatements{}, err
	}
	createTrigger := "CREATE TRIGGER " + trigger + " AFTER " + strings.ToUpper(operation.Kind) + columns + " ON " + target + " FOR EACH ROW" + whenClause(operation) + " EXECUTE FUNCTION " + function + "();"
	createFunction := "CREATE OR REPLACE FUNCTION " + function + "() RETURNS trigger\n  LANGUAGE plpgsql\n  SECURITY DEFINER\n  SET search_path = pg_catalog, pg_temp\nAS $" + tag + "$\n" + body + "\n$" + tag + "$;"
	return triggerStatements{
		createFunction:  createFunction,
		revokeExecute:   "REVOKE EXECUTE ON FUNCTION " + function + "() FROM PUBLIC;",
		commentFunction: "COMMENT ON FUNCTION " + function + "() IS " + markerText + ";",
		createTrigger:   createTrigger,
		commentTrigger:  "COMMENT ON TRIGGER " + trigger + " ON " + target + " IS " + markerText + ";",
	}, nil
}

func updateColumns(operation config.Operation) (string, error) {
	if operation.Kind != "update" || len(operation.Columns) == 0 {
		return "", nil
	}
	quoted := make([]string, 0, len(operation.Columns))
	for _, column := range operation.Columns {
		name, reason := schema.Quoted(column)
		if reason != schema.IdentifierOK {
			return "", unusableIdentifierError{field: "update column", value: column, reason: reason}
		}
		quoted = append(quoted, name)
	}
	return " OF " + strings.Join(quoted, ", "), nil
}

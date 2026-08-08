package source

import (
	"strings"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
)

// The design accepts an N-rebuild subtraction chain for full/exclude rather than reopening
// PostgreSQL's third array-literal grammar. Columns go through schema.Quoted; JSON keys go through
// quoteLiteral.
// The operation branches deliberately scope include_old: inserts have no OLD row, while deletes'
// OLD row is the only row available, so neither path reads the flag.
type payloadExpressions struct {
	new string
	old string
}

func payloadExpressionsFor(operation string, listener config.Listener, target Target) (payloadExpressions, error) {
	if _, err := operationAbbreviationFor(operation); err != nil {
		return payloadExpressions{}, err
	}
	if err := payloadModeKnown(listener.Trigger.Payload.Mode); err != nil {
		return payloadExpressions{}, err
	}
	newExpression, err := newPayloadExpression(listener.Trigger.Payload, target, listener.Name)
	if err != nil {
		return payloadExpressions{}, err
	}
	if operation == "delete" {
		newExpression = "NULL::jsonb"
	}
	oldExpression, err := oldPayloadExpression(operation, listener.Trigger.Payload)
	if err != nil {
		return payloadExpressions{}, err
	}
	return payloadExpressions{
		new: newExpression, old: oldExpression,
	}, nil
}

func newPayloadExpression(payload config.Payload, target Target, listener string) (string, error) {
	switch payload.Mode {
	case "full":
		return fullPayloadExpression(payload.Exclude)
	case "columns":
		return objectPayloadExpression(payload.Columns, "NEW")
	case "keys_only":
		if len(target.PrimaryKeyColumns) == 0 {
			return "", missingPrimaryKey(listener)
		}
		return objectPayloadExpression(target.PrimaryKeyColumns, "NEW")
	default:
		return "", unknownPayloadModeError{mode: payload.Mode}
	}
}

func fullPayloadExpression(exclude []string) (string, error) {
	expression := "to_jsonb(NEW)"
	for _, column := range exclude {
		key, err := payloadKey(column)
		if err != nil {
			return "", err
		}
		expression += " - " + key
	}
	return expression, nil
}

func objectPayloadExpression(columns []string, row string) (string, error) {
	parts := make([]string, 0, len(columns)*2)
	for _, column := range columns {
		key := quoteLiteral(column)
		quoted, reason := schema.Quoted(column)
		if reason != schema.IdentifierOK {
			return "", unusableIdentifierError{field: "payload column", value: column, reason: reason}
		}
		parts = append(parts, key, row+"."+quoted)
	}
	return "jsonb_build_object(" + strings.Join(parts, ", ") + ")", nil
}

func payloadKey(column string) (string, error) {
	if err := identifierRefusal("payload column", column); err != nil {
		return "", err
	}
	return quoteLiteral(column), nil
}

func oldPayloadExpression(operation string, payload config.Payload) (string, error) {
	switch operation {
	case "insert":
		return "NULL::jsonb", nil
	case "delete":
	case "update":
		if !payload.IncludeOld {
			return "NULL::jsonb", nil
		}
	default:
		return "NULL::jsonb", nil
	}
	if payload.Mode == "columns" {
		return objectPayloadExpression(payload.Columns, "OLD")
	}
	return "to_jsonb(OLD)", nil
}

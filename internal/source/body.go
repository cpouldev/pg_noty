package source

import (
	"strings"

	"github.com/cpouldev/pg_noty/internal/config"
	"github.com/cpouldev/pg_noty/internal/schema"
)

// triggerBody carries three deliberate corrections. One clock value is returned with the event id
// and reused for the composite queue foreign key and next_attempt_at; coalesce handles the measured
// transaction-local empty-string reset; and there is no EXCEPTION block because one subtransaction
// per row overflows PostgreSQL's PGPROC 64-subxid cache and harms visibility server-wide.
func triggerBody(request Request, operation config.Operation) (string, error) {
	events, err := serviceTable(request.ServiceSchema, schema.TableEvents)
	if err != nil {
		return "", err
	}
	queue, err := serviceTable(request.ServiceSchema, schema.TableEventQueue)
	if err != nil {
		return "", err
	}
	target, reason := schema.Qualified(request.Target.Schema, request.Target.Table)
	if reason != schema.IdentifierOK {
		return "", unusableIdentifierError{field: "target", value: request.Target.Table, reason: reason}
	}
	payload, err := payloadExpressionsFor(operation.Kind, request.Listener, request.Target)
	if err != nil {
		return "", err
	}
	listener := quoteLiteral(request.Listener.Name)
	storedTable := quoteLiteral(target)
	operationName := quoteLiteral(operation.Kind)
	channel := quoteLiteral("pg_noty_events_" + request.Instance)
	guard := "  IF coalesce(current_setting(" + quoteLiteral("pg_noty.n") + ", true), " + quoteLiteral("") + ") = " + quoteLiteral("") + " THEN"
	return strings.Join(
		[]string{
			"BEGIN", "  WITH e AS (",
			"    INSERT INTO " + events + " (listener, table_name, operation, payload, txid, occurred_at)",
			"    VALUES (" + listener + ", " + storedTable + ", " + operationName + ",",
			"            jsonb_build_object(" + quoteLiteral("old") + ", " + payload.old + ", " + quoteLiteral("new") + ", " + payload.new + "),",
			"            pg_current_xact_id(), clock_timestamp())", "    RETURNING id, occurred_at", "  )",
			"  INSERT INTO " + queue, "         (event_id, occurred_at, listener, status, attempts, next_attempt_at)",
			"  SELECT e.id, e.occurred_at, " + listener + ", " + quoteLiteral("pending") + ", 0, e.occurred_at FROM e;",
			"",
			guard, "    PERFORM pg_notify(" + channel + ", " + quoteLiteral("") + ");",
			"    PERFORM set_config(" + quoteLiteral("pg_noty.n") + ", " + quoteLiteral("1") + ", true);", "  END IF;",
			"  RETURN NULL;", "END",
		}, "\n",
	), nil
}

func serviceTable(serviceSchema, table string) (string, error) {
	qualified, reason := schema.Qualified(serviceSchema, table)
	if reason != schema.IdentifierOK {
		return "", unusableIdentifierError{field: "service schema", value: serviceSchema, reason: reason}
	}
	return qualified, nil
}

package delivery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cpouldev/pg_noty/internal/source"
)

// BuildEnvelope serializes one source event into the stable public webhook body.
// Payload bytes are copied into the data member without decoding or re-encoding;
// consequently their key order and whitespace are part of the signed body.
func BuildEnvelope(event source.Event) ([]byte, error) {
	schemaName, tableName, ok := source.SplitQualified(event.Table)
	if !ok {
		return nil, fmt.Errorf("event table is not a quoted qualified name: %q", event.Table)
	}
	payload := event.Payload
	if len(payload) == 0 {
		return nil, errors.New("event payload is empty")
	}
	if !json.Valid(payload) {
		return nil, errors.New("event payload is not valid JSON")
	}
	var body bytes.Buffer
	body.WriteString(`{"pg_noty":"1","id":`)
	body.WriteString(strconv.FormatInt(event.ID, 10))
	body.WriteString(`,"listener":`)
	if err := writeJSONString(&body, event.Listener); err != nil {
		return nil, err
	}
	body.WriteString(`,"table":{"schema":`)
	if err := writeJSONString(&body, schemaName); err != nil {
		return nil, err
	}
	body.WriteString(`,"name":`)
	if err := writeJSONString(&body, tableName); err != nil {
		return nil, err
	}
	body.WriteString(`},"op":`)
	if err := writeJSONString(&body, event.Operation); err != nil {
		return nil, err
	}
	body.WriteString(`,"occurred_at":`)
	if err := writeJSONString(&body, occurredAt(event.OccurredAt)); err != nil {
		return nil, err
	}
	body.WriteString(`,"txid":`)
	body.WriteString(strconv.FormatUint(event.TXID, 10))
	body.WriteString(`,"data":`)
	body.Write(payload)
	body.WriteByte('}')
	return body.Bytes(), nil
}

func writeJSONString(body *bytes.Buffer, value string) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = body.Write(encoded)
	return err
}

func occurredAt(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000Z07:00")
}

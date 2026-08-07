package source

// PostgreSQL has other trigger events, but TRUNCATE is deliberately not in this vocabulary:
// FOR EACH ROW cannot be created for TRUNCATE, so a watched table's TRUNCATE produces no events.
// Keep this value closed and ordered like config's canonical operation order. An addition in
// internal/config must join that declaration and this table or the source drift pin fails.
type operationAbbreviation struct {
	kind         string
	abbreviation string
}

var operationAbbreviations = [...]operationAbbreviation{
	{kind: "insert", abbreviation: "ins"},
	{kind: "update", abbreviation: "upd"},
	{kind: "delete", abbreviation: "del"},
}

type unknownOperationError struct{ kind string }

func (e unknownOperationError) Error() string {
	return "unknown operation " + e.kind + ": no trigger abbreviation"
}

func operationAbbreviationFor(kind string) (string, error) {
	for _, entry := range operationAbbreviations {
		if entry.kind == kind {
			return entry.abbreviation, nil
		}
	}
	return "", unknownOperationError{kind: kind}
}

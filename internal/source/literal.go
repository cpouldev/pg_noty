package source

import "strings"

// quoteLiteral is the package's sole literal-emission authority. It mirrors PostgreSQL's
// quote_literal grammar without a server round trip: generation stays offline, so the
// catalog round-trip helper in internal/schema cannot be used here. A backslash requires E” with
// both quotes and backslashes doubled; otherwise the plain literal grammar is sufficient.
func quoteLiteral(value string) string {
	if strings.Contains(value, `\`) {
		value = strings.ReplaceAll(value, `\`, `\\`)
		value = strings.ReplaceAll(value, "'", "''")
		return "E'" + value + "'"
	}
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

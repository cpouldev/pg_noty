package delivery

const maxSnippetBytes = 2 << 10

// TruncateSnippet bounds response text at the database's 2 KiB byte contract. It does not make the
// result storable: that is a question about the column, and internal/source answers it for every
// text parameter it writes (storabletext.go).
func TruncateSnippet(body []byte) string {
	if len(body) <= maxSnippetBytes {
		return string(body)
	}
	return string(body[:maxSnippetBytes])
}

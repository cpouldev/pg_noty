package source

import (
	"fmt"
	"strings"
)

// dollarQuoteTag chooses a deterministic tag after the rendered body contains every substituted
// identifier and literal. The base fn then ascends; identifierDefect deliberately permits '$', so
// choosing from a template before substitution would miss a collision inside a quoted identifier.
func dollarQuoteTag(renderedBody string) string {
	for suffix := 0; ; suffix++ {
		tag := "fn"
		if suffix > 0 {
			tag = fmt.Sprintf("fn_%d", suffix)
		}
		if !strings.Contains(renderedBody, "$"+tag+"$") {
			return tag
		}
	}
}

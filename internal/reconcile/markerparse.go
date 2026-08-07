package reconcile

import (
	"strings"

	"github.com/cpouldev/pg_noty/internal/schema"
)

// catalogMarker is the marker grammar source.Generate writes and reconciliation reads back.
// markerparse.go is this package's only marker splitter; schema.MarkerPrefix is its only prefix.
type catalogMarker struct {
	Instance  string
	Listener  string
	Operation string
}

// parseCatalogMarker accepts only the complete five-field marker grammar. It fail-closes before
// ownership can ask whether a malformed comment looks sufficiently close to a marker.
func parseCatalogMarker(found *string) (catalogMarker, bool) {
	if found == nil {
		return catalogMarker{}, false
	}
	rest, hasPrefix := strings.CutPrefix(*found, schema.MarkerPrefix)
	if !hasPrefix {
		return catalogMarker{}, false
	}
	fields := strings.Split(rest, ":")
	if len(fields) != 3 || hasEmptyMarkerField(fields) {
		return catalogMarker{}, false
	}
	return catalogMarker{Instance: fields[0], Listener: fields[1], Operation: fields[2]}, true
}

func hasEmptyMarkerField(fields []string) bool {
	for _, field := range fields {
		if field == "" || strings.Contains(field, ":") {
			return true
		}
	}
	return false
}

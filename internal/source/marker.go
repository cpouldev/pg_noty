package source

import (
	"github.com/cpouldev/pg_noty/internal/schema"
)

// marker is a comment body, not a PostgreSQL identifier. The 63-byte identifier bound therefore
// has no bearing on it: pg_description.description is text, and truncating this body would destroy
// the listener and operation fields internal/reconcile uses for ownership. MarkerPrefix remains
// schema's one authority for the composed pg_noty marker prefix.
func marker(instance, listener, operation string) string {
	return schema.MarkerPrefix + instance + ":" + listener + ":" + operation
}

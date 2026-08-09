package source

import "github.com/cpouldev/pg_noty/internal/schema"

// These are the four refusal classes generation can expose. The fixed inventory makes a new
// failure class join an assertion instead of becoming an unlabelled partial-rendering path.
type refusalKind string

var refusalKinds = [...]refusalKind{
	"unusable identifier", "missing primary key", "unknown operation", "unknown payload mode",
}

type unusableIdentifierError struct {
	field  string
	value  string
	reason schema.IdentifierFault
}

func (e unusableIdentifierError) Error() string {
	return "unusable " + e.field + " " + e.value + ": " + string(e.reason)
}

type missingPrimaryKeyError struct{ listener string }

func (e missingPrimaryKeyError) Error() string {
	return "listener " + e.listener + " has no primary key for keys_only"
}

// identifierRefusal is this package's identifier boundary. Its reachability is worth stating here
// rather than leaving to a reader to reconstruct: pg_class.relname and pg_attribute.attname are of
// PostgreSQL's `name` type, which is NUL-terminated and capped at 63 bytes, so a catalog read
// cannot hand this function an over-limit or NUL-bearing identifier at all. Every value that trips
// the branch below therefore comes from a caller that did not read the catalog -- a hand-built
// Request, a resolver that transcribed rather than queried, a future route. That is exactly why the
// tests for this branch construct their inputs here instead of claiming they arrived from a catalog
// that cannot hold them; the branch is reached directly by TestIdentifierRefusalNamesValueAndReason
// and TestIdentifierBoundaryRefusesOnlyPastTheLimit.
func identifierRefusal(field, value string) error {
	reason := schema.WhyUnusable(value)
	if reason == schema.IdentifierOK {
		return nil
	}
	return unusableIdentifierError{field: field, value: value, reason: reason}
}

func validateTargetIdentifiers(target Target) error {
	for _, field := range []struct{ name, value string }{
		{"target schema", target.Schema}, {"target table", target.Table},
	} {
		if err := identifierRefusal(field.name, field.value); err != nil {
			return err
		}
	}
	for _, column := range target.PrimaryKeyColumns {
		if err := identifierRefusal("primary key column", column); err != nil {
			return err
		}
	}
	return nil
}

func validateRequestIdentifiers(request Request) error {
	for _, field := range []struct{ name, value string }{
		{"service schema", request.ServiceSchema}, {"listener", request.Listener.Name},
	} {
		if err := identifierRefusal(field.name, field.value); err != nil {
			return err
		}
	}
	return validateTargetIdentifiers(request.Target)
}

func missingPrimaryKey(listener string) error {
	return missingPrimaryKeyError{listener: listener}
}

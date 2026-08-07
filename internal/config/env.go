package config

import "maps"

// EnvLookup resolves an environment variable reference. It is signature-compatible
// with os.LookupEnv, so production code passes the standard library function
// unmodified while tests inject a map.
//
// The (string, bool) result is what keeps three states apart, which the
// interpolation grammar depends on: unset ("", false), set to the empty string
// ("", true), and set to a value. ${NAME:-default} substitutes its default for the
// first two; ${NAME} on an unset variable is a diagnostic rather than an empty
// value.
type EnvLookup func(name string) (value string, ok bool)

// MapEnv returns an EnvLookup backed by the given variables. A nil or empty map is
// a valid environment in which every reference is unset.
func MapEnv(vars map[string]string) EnvLookup {
	// The map is copied so that mutating the caller's map after construction cannot
	// change what a reference resolves to. One run must see one environment. A nil map
	// clones to nil, which reads as the empty environment because reading a nil map
	// yields the zero value and false -- exactly what an unset variable means here.
	frozen := maps.Clone(vars)

	return func(name string) (string, bool) {
		value, ok := frozen[name]
		return value, ok
	}
}

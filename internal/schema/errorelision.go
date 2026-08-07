package schema

import (
	"errors"
	"strings"
)

// This file is ADR-11's second half: the one place an error leaves this package, and the only place
// a credential could leave with it.
//
// The password is elided rather than searched for. internal/config's passwordSpans family answers a
// different question -- find a password inside arbitrary author-written text -- and is unexported
// besides, so a copy would be both the wrong tool and unreachable. Step 7's pool.go parses the DSN
// exactly once and hands the password it read to every finished error, which is a smaller and exact
// claim: this text must not contain that string.

// elidedPassword replaces a password wherever it is written.
const elidedPassword = "***"

// elidedError is an error that has left this package. It carries the rendered text and, when the
// error it was built from matched one, the sentinel a caller tests against.
//
// It unwraps to that sentinel and to nothing else, which is the point rather than a simplification.
// Measured on github.com/jackc/pgx/v5 v5.10.0, *pgconn.ParseConfigError holds the connection string
// in an exported ConnString field, unredacted, whatever its Error method renders. A chain that
// stayed unwrappable back to the driver's error would hand a caller everything the text had just
// removed (TestTheFinishedErrorCannotBeUnwrappedBackToTheDriversCopyOfTheDSN).
type elidedError struct {
	text     string
	sentinel error
}

func (e *elidedError) Error() string { return e.text }

func (e *elidedError) Unwrap() error { return e.sentinel }

// finished is the one place an error leaves this package. Every exported function that returns an
// error returns one of these, and a driver-reaching source that returns an error without calling it
// fails TestNoDriverReachingSourceReturnsAnErrorWithoutFinishingIt.
//
// secrets is every password this package knows -- more than one because libpq accepts a repeated
// `password=` keyword and takes the last, so an earlier one is a second credential rather than a
// stale copy, and both have to go.
func finished(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	return &elidedError{
		text:     withoutSecrets(err.Error(), secrets),
		sentinel: sentinelMatching(err),
	}
}

// withoutSecrets replaces every occurrence of every known secret. Every occurrence rather than the
// first, because one connection string can carry a password in its userinfo and again as a
// parameter, and the driver's own error redacts only the first of those.
//
// A secret that also spells a host or a database name takes that with it. Over-redaction is the
// safe direction and is deliberate: the alternative is deciding which occurrence is the credential,
// which is exactly the question ADR-11 avoided by having the password handed in.
func withoutSecrets(text string, secrets []string) string {
	for _, secret := range secrets {
		// An unknown password is not a licence to rewrite the message: replacing the empty string
		// would put the placeholder between every rune of it.
		if secret == "" {
			continue
		}
		text = strings.ReplaceAll(text, secret, elidedPassword)
	}
	return text
}

// sentinelMatching is the member of the vocabulary an error matches, or nil. It ranges over the
// declared set, so a sentinel added there is carried through the finishing point without this
// function being touched.
func sentinelMatching(err error) error {
	for _, sentinel := range packageSentinels {
		if errors.Is(err, sentinel) {
			return sentinel
		}
	}
	return nil
}

package config

// This file is the document as written: one decode target per mapping level the contract declares,
// with the wrappers of scalar.go and rawcontainer.go standing in for every value. It mirrors the YAML
// shape and nothing else -- no default is applied here and no constraint is judged, because both
// belong to stages after this one, and a raw struct that resolved a default would make "the author
// wrote this" unanswerable.
//
// It is a second struct family beside the resolved public types in config.go, which ADR-2 names as the
// cost of the mechanism: the public types are plainly typed because a consumer must not have to ask
// whether a value was written, and these are wrapped because this package must.
//
// Every `yaml` tag here is reconciled against schema.go's key lists by drift_test.go, in both
// directions, so a key that is checkable but not decodable -- or decodable but never checked -- fails
// a test rather than becoming a silently ignored line in someone's configuration file.
//
// The names differ from the step's Expected Output in one place, and deliberately: it calls the two
// container unmarshalers `Operations` and `Headers`, and config.go already owns both names as public
// resolved types. They are `rawOperations` and `rawHeaders` here, which is also what "unexported
// decode targets" asks for.

// rawConfig is the document root.
type rawConfig struct {
	Version       Int                     `yaml:"version"`
	Instance      Str                     `yaml:"instance"`
	AutoReconcile Bool                    `yaml:"auto_reconcile"`
	Database      mappingOf[rawDatabase]  `yaml:"database"`
	Worker        mappingOf[rawWorker]    `yaml:"worker"`
	Retention     mappingOf[rawRetention] `yaml:"retention"`
	Defaults      mappingOf[rawDefaults]  `yaml:"defaults"`
	Listeners     listOf[rawListener]     `yaml:"listeners"`
}

type rawDatabase struct {
	URL       Str `yaml:"url"`
	Schema    Str `yaml:"schema"`
	ListenURL Str `yaml:"listen_url"`
}

type rawWorker struct {
	Concurrency  Int `yaml:"concurrency"`
	BatchSize    Int `yaml:"batch_size"`
	PollInterval Dur `yaml:"poll_interval"`
	LeaseTimeout Dur `yaml:"lease_timeout"`
	DrainTimeout Dur `yaml:"drain_timeout"`

	AllowedDestinationCIDRs StrList `yaml:"allowed_destination_cidrs"`
}

type rawRetention struct {
	Keep              Dur `yaml:"keep"`
	PartitionInterval Dur `yaml:"partition_interval"`
	Precreate         Dur `yaml:"precreate"`
}

// rawDefaults is merge layer 2. It holds no listener key of its own: what a listener may inherit is
// exactly what the contract declares here, which is why the level is declared once in schema.go and
// read from there by the drift test.
type rawDefaults struct {
	Timeout Dur                 `yaml:"timeout"`
	Retry   mappingOf[rawRetry] `yaml:"retry"`
	Headers rawHeaders          `yaml:"headers"`
}

type rawRetry struct {
	MaxAttempts     Int  `yaml:"max_attempts"`
	Backoff         Str  `yaml:"backoff"`
	InitialInterval Dur  `yaml:"initial_interval"`
	MaxInterval     Dur  `yaml:"max_interval"`
	Jitter          Bool `yaml:"jitter"`
}

// rawListener is one listener as written. The trigger-affecting and delivery-affecting split of
// ADR-8 is not made here: it is a property of the resolved type a consumer reads, and imposing it on
// the decode target would put a grouping the file does not have between an author's line and its
// diagnostic.
type rawListener struct {
	Name        Str                       `yaml:"name"`
	Enabled     Bool                      `yaml:"enabled"`
	Table       Str                       `yaml:"table"`
	Operations  rawOperations             `yaml:"operations"`
	Payload     mappingOf[rawPayload]     `yaml:"payload"`
	Destination mappingOf[rawDestination] `yaml:"destination"`
	Retry       mappingOf[rawRetry]       `yaml:"retry"`
	Timeout     Dur                       `yaml:"timeout"`
	Concurrency Int                       `yaml:"concurrency"`
}

type rawPayload struct {
	Mode       Str     `yaml:"mode"`
	Columns    StrList `yaml:"columns"`
	Exclude    StrList `yaml:"exclude"`
	IncludeOld Bool    `yaml:"include_old"`
	MaxBytes   Int     `yaml:"max_bytes"`
}

type rawDestination struct {
	URL     Str                   `yaml:"url"`
	Method  Str                   `yaml:"method"`
	Headers rawHeaders            `yaml:"headers"`
	Signing mappingOf[rawSigning] `yaml:"signing"`
}

type rawSigning struct {
	Secrets StrList `yaml:"secrets"`
}

// rawOperation is one statement a listener reacts to, with the filters that apply to that statement.
// Its name is not a field: it is the key the author wrote, carried by rawOperations as a positioned
// text of its own so that a diagnostic about an operation anchors on that key (Step 5's Note 15).
type rawOperation struct {
	Columns StrList `yaml:"columns"`
	When    Str     `yaml:"when"`
}

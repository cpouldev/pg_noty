// Package config defines and validates pg_noty's YAML contract. This
// reference covers configuration loading only. Planning, applying database
// changes, delivery, and retention pruning belong to later packages.
//
// # Reference conventions
//
// The Default column contains all 22 enumerated built-in defaults exactly once.
// "Required" means omission is an error. "None" means the key is optional and
// has no enumerated built-in default; nested defaults and requirements still
// apply when an optional mapping is omitted. Defaults are applied field by
// field in this order: built-in values, `defaults`, then the listener.
//
// Exactly seven keys are required: `version`, `database.url`, `listeners`,
// and each listener's `name`, `table`, `operations`, and `destination.url`.
// An explicit `listeners: []` is valid; an absent `listeners` key is not.
//
// Every value must have the YAML shape and scalar type shown below. Every
// mapping rejects duplicate keys (R42). Header mappings accept free-form names.
// Unsupported operation names violate R28. Every other mapping rejects unknown
// keys under R41.
//
// # Configuration keys
//
//	Path                                                     Type                         Default                         Constraints
//	`version`                                                integer                      required                        R1: equals 1
//	`instance`                                               string                       built-in: `database.schema`    R2: `^[a-z][a-z0-9_]{0,40}$` (1-41 characters)
//	`auto_reconcile`                                         boolean                      built-in: `false`               when false, `run` verifies the schema and triggers and refuses to serve rather than creating or changing them
//	`database`                                               mapping                      none                            omitting nested `url` violates R3; R41, R42
//	`database.url`                                           string                       required                        R3: non-empty PostgreSQL URI or libpq keyword/value connection string
//	`database.schema`                                        string                       built-in: `noty`                R4: PostgreSQL unquoted identifier, at most 63 bytes, without the reserved `pg_` prefix
//	`database.listen_url`                                    string                       none                            R5: when present, the same connection-string forms as `database.url`
//	`worker`                                                 mapping                      none                            R41, R42
//	`worker.concurrency`                                     integer                      built-in: `16`                  R6: 1-1024
//	`worker.batch_size`                                      integer                      built-in: `100`                 R7: 1-10000
//	`worker.poll_interval`                                   Go duration                   built-in: `10s`                 R8, R40: positive
//	`worker.lease_timeout`                                   Go duration                   built-in: `5m`                  R8, R9, R40: positive and greater than the largest effective listener timeout
//	`worker.allowed_destination_cidrs`                       sequence of strings          none                            each entry must parse as a CIDR; internal/delivery refuses the whole set rather than dropping one
//	`worker.drain_timeout`                                   Go duration                   built-in: `30s`                 R8, R40: positive; W2 warns when below the largest effective listener timeout
//	`retention`                                              mapping                      none                            R41, R42
//	`retention.keep`                                         Go duration                   built-in: `168h`                R10, R11, R40: positive and at least `partition_interval`
//	`retention.partition_interval`                           Go duration                   built-in: `24h`                 R10, R40: positive
//	`retention.precreate`                                    Go duration                   built-in: `168h`                R10, R12, R40: positive and at least `partition_interval`
//	`defaults`                                               mapping                      none                            optional merge layer; R41, R42
//	`defaults.timeout`, `listeners[].timeout`                Go duration                   built-in: `5s`                  R13, R40: positive
//	`defaults.retry`, `listeners[].retry`                    mapping                      none                            field-level merge; R41, R42
//	`defaults.retry.max_attempts`, `listeners[].retry.max_attempts`  integer                    built-in: `5`                   R14: at least 1
//	`defaults.retry.backoff`, `listeners[].retry.backoff`    string                       built-in: `exponential`         R15: `exponential`, `linear`, or `fixed`
//	`defaults.retry.initial_interval`, `listeners[].retry.initial_interval`  Go duration        built-in: `10s`                 R16, R40: positive and at most `max_interval`
//	`defaults.retry.max_interval`, `listeners[].retry.max_interval`  Go duration                built-in: `1h`                  R17, R40: positive
//	`defaults.retry.jitter`, `listeners[].retry.jitter`      boolean                      built-in: `true`                R18
//	`defaults.headers`, `listeners[].destination.headers`    mapping of string to string  none                            R19-R21: valid HTTP names, non-empty values, no case-insensitive duplicates or reserved `X-Pg-Noty-` names
//	`listeners`                                              sequence of mappings         required                        R22: key must be present; an empty sequence is valid
//	`listeners[].name`                                       string                       required                        R23, R24: name pattern from R2 and unique across listeners
//	`listeners[].enabled`                                    boolean                      built-in: `true`                R25; disabled listeners still receive full static validation
//	`listeners[].table`                                      string                       required                        R26: `schema.table`; each part is an unquoted identifier of at most 63 bytes
//	`listeners[].operations`                                 mapping or sequence          required                        R27-R29: at least one unique `insert`, `update`, or `delete`; sequence form is filter-free sugar
//	`listeners[].operations.insert`                          mapping                      none                            R28: optional operation; R41, R42
//	`listeners[].operations.update`                          mapping                      none                            R28: optional operation; R41, R42
//	`listeners[].operations.delete`                          mapping                      none                            R28: optional operation; R41, R42
//	`listeners[].operations.<op>.columns`                    sequence of strings          none                            R29: only under `update`; non-empty, unique valid identifiers
//	`listeners[].operations.<op>.when`                       string                       none                            R30: no `OLD` for `insert`, no `NEW` for `delete`
//	`listeners[].payload`                                    mapping                      none                            nested defaults apply; R41, R42
//	`listeners[].payload.mode`                               string                       built-in: `full`                R31: `full`, `columns`, or `keys_only`
//	`listeners[].payload.columns`                            sequence of strings          none                            R32: present iff `mode: columns`; non-empty, unique valid identifiers
//	`listeners[].payload.exclude`                            sequence of strings          none                            R33: only for `mode: full`; mutually exclusive with `columns`; unique valid identifiers
//	`listeners[].payload.include_old`                        boolean                      built-in: `false`               R34
//	`listeners[].payload.max_bytes`                          integer                      built-in: `262144`              R35: greater than 0
//	`listeners[].destination`                                mapping                      none                            omitting nested `url` violates R36; R41, R42
//	`listeners[].destination.url`                            string                       required                        R36: non-empty absolute `http` or `https` URL with a host
//	`listeners[].destination.method`                         string                       built-in: `POST`                R37: `POST`, `PUT`, or `PATCH`
//	`listeners[].destination.signing`                        mapping                      none                            R41, R42
//	`listeners[].destination.signing.secrets`                sequence of strings          none                            R38: non-empty unique entries; W1 warns when an entry is a YAML literal
//	`listeners[].concurrency`                                integer                      none                            R39: when present, 1-1024 and at most `worker.concurrency`
//
// Header names are stored canonically. Listener destination headers override
// `defaults.headers` case-insensitively. PostgreSQL connection URIs begin with
// the exact `postgres://` or `postgresql://` designator; the keyword/value form
// follows libpq's grammar. PostgreSQL object identifiers may contain non-ASCII
// letters. The `pg_` prohibition applies only to `database.schema`, not to the
// schema part of `listeners[].table`.
//
// # Interpolation grammar
//
// Interpolation applies to scalar values, never mapping keys. `${NAME}` requires
// a set variable. `${NAME:-default}` uses `default` when the variable is unset or
// empty. NAME matches `[A-Za-z_][A-Za-z0-9_]*`; default text cannot contain `}`,
// and references do not nest. `$${` is the only escape and produces a literal
// `${`; a lone `$` and bare `$$` remain literal. Substituted bytes are opaque and
// are never parsed again as YAML. Every other unescaped `${...}` spelling,
// including `${1VAR}`, `${}`, and an unterminated reference, is a diagnostic.
//
// `${NAME:-}` is legal and deliberately requests an empty result. It distinguishes
// "I mean empty" from a missing variable, which is never silently empty. The
// receiving field may still reject that empty result.
//
// # Go-units-only duration position and the carried-forward d/w question
//
// The interim position is Go units only: R40 accepts the unit set in
// DocumentedDurationUnits. Thus `precreate: 168h` is valid and `7d` is rejected
// with a `168h` suggestion. The carried-forward `d`/`w` question remains open:
// should `d` (=24h) and `w` (=168h) become fixed convenience multiples? Fixed
// days differ from calendar days across daylight-saving transitions. A later
// decision must widen the package's single duration parser/source and this one
// documented value together.
//
// # The when trust boundary
//
// `when:` is raw SQL embedded in a trigger. Anyone who can edit the YAML can
// execute arbitrary SQL inside the writing transaction for the target table.
// Interpolation in `when` extends that authority to whoever supplies the
// referenced environment variable. R30 is only a lexical OLD/NEW pre-check; it
// is not a SQL parser or a sandbox.
//
// # Resolved validation boundaries
//
// `payload.mode: keys_only` passes static validation.
// Its primary-key requirement is a deferred catalog check through DBValidator.
// internal/reconcile must run that check before issuing DDL. "Report all errors"
// applies after parsing: read and YAML parse failures are fatal-single,
// interpolation accumulates and then stops, and structural, decode, and semantic
// validation accumulate.
package config

// DocumentedDurationUnits is the single machine-referable rendering of R40's
// accepted-unit list. The Step 12a reconciliation test compares it with the
// package's single duration-unit parser/source.
const DocumentedDurationUnits = durationUnitFamilies

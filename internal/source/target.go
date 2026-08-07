package source

import "github.com/cpouldev/pg_noty/internal/config"

// Target is resolved catalog metadata. Its fields carry no validation guarantee:
// internal/reconcile's PrimaryKeyPresent check refuses a target without a primary key before DDL,
// while Generate owns the separate refusal for an empty PrimaryKeyColumns list under keys_only.
type Target struct {
	Schema            string
	Table             string
	PrimaryKeyColumns []string
}

type Request struct {
	Instance      string
	ServiceSchema string
	Listener      config.Listener
	Target        Target
}

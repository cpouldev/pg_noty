package source

// ObjectSet is the closed inventory internal/reconcile hashes and reconciles. Operation is explicit
// even though the set reads at first as eight text artifacts: using a slice ordinal as operation
// identity would make listener_triggers' (listener, operation) key depend on position.
type ObjectSet struct {
	Operation       string
	FunctionName    string
	TriggerName     string
	Marker          string
	CreateFunction  string
	RevokeExecute   string
	CommentFunction string
	CreateTrigger   string
	CommentTrigger  string
}

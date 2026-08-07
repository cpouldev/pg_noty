package source

import "time"

// Event is the mechanism-free event vocabulary shared by every source adapter.
type Event struct {
	ID         int64
	Listener   string
	Operation  string
	OccurredAt time.Time
	Table      string
	TXID       uint64
	Payload    []byte
	Attempt    int
}

// Delivery contains the result of delivering one Event; it is source-independent vocabulary.
type Delivery struct {
	HTTPStatus int
	Snippet    string
	Err        string
	Duration   time.Duration
}

package source

import "errors"

// ErrEventNotClaimed means a transition targeted no currently claimed queue row; callers may
// treat the late acknowledgement as an idempotent delivery outcome.
var ErrEventNotClaimed = errors.New("event not claimed")

// ErrSourceClosed means the source lifecycle has ended. Context cancellation is not translated
// into either sentinel: errors.Is against context.Canceled remains the cancellation classification.
var ErrSourceClosed = errors.New("event source closed")

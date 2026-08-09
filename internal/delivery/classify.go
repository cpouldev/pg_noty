package delivery

// Outcome is the closed result set consumed by the worker after one request.
type Outcome uint8

const (
	OutcomeTerminal Outcome = iota
	OutcomeSuccess
	OutcomeRetryable
)

// String gives dead-letter reasons and diagnostics a stable vocabulary.
func (o Outcome) String() string {
	switch o {
	case OutcomeSuccess:
		return "success"
	case OutcomeRetryable:
		return "retryable"
	default:
		return "terminal"
	}
}

// ClassifyStatus is total for every integer HTTP status. Only 2xx succeeds;
// 408, 429 and 5xx are retryable. Every 3xx, other 4xx, 1xx, 6xx and negative
// value fails closed as terminal.
func ClassifyStatus(status int) Outcome {
	if status >= 200 && status <= 299 {
		return OutcomeSuccess
	}
	if status == 408 || status == 429 || status >= 500 && status <= 599 {
		return OutcomeRetryable
	}
	return OutcomeTerminal
}

// ClassifyError maps a transport failure to the retryable arm. A nil error is
// not a response, so it is terminal rather than silently becoming success.
func ClassifyError(err error) Outcome {
	if err != nil {
		return OutcomeRetryable
	}
	return OutcomeTerminal
}

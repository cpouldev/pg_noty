package delivery

import "fmt"

func checkPayloadSize(payload []byte, limit int) (bool, string) {
	if len(payload) <= limit {
		return true, ""
	}
	return false, fmt.Sprintf("payload size %d exceeds limit %d", len(payload), limit)
}

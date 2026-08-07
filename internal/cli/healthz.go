package cli

import (
	"net/http"
	"time"
)

func healthz(start time.Time) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if time.Since(start) < 0 {
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok\n"))
	})
}

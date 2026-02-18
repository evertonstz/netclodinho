package server

import (
	"net/http"

	httptrace "github.com/DataDog/dd-trace-go/contrib/net/http/v2"

	"github.com/angristan/netclode/services/github-bot/internal/webhook"
)

// New creates a new HTTP server mux with the webhook and health endpoints.
// The returned handler is wrapped with Datadog tracing.
func New(handler *webhook.Handler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.Handle("POST /webhook", handler)

	return httptrace.WrapHandler(mux, "github-bot", "http.request")
}

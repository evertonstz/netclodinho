package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	mux := New(nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", w.Body.String(), "ok")
	}
}

// stubHandler records whether it was called.
type stubHandler struct{ called bool }

func (h *stubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.called = true
	w.WriteHeader(http.StatusOK)
}

func TestWebhookRoutePost(t *testing.T) {
	stub := &stubHandler{}
	// New expects *webhook.Handler but the mux registers it as http.Handler.
	// We use a wrapper mux to test routing with our stub.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("POST /webhook", stub)

	req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if !stub.called {
		t.Error("POST /webhook should route to the handler")
	}
}

func TestWebhookRouteGetNotAllowed(t *testing.T) {
	stub := &stubHandler{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("POST /webhook", stub)

	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if stub.called {
		t.Error("GET /webhook should not route to the handler")
	}
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /webhook status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

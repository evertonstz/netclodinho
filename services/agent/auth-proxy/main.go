// auth-proxy is a tiny HTTP proxy that adds ServiceAccount token authentication.
//
// It sits between the SDK HTTP client and the external secret-proxy:
//
//	SDK → auth-proxy (localhost:8080) → secret-proxy (external)
//
// The auth-proxy:
// 1. Reads the K8s ServiceAccount token from a mounted file
// 2. Adds Proxy-Authorization: Bearer <token> to each request
// 3. Forwards to the external secret-proxy
//
// This proxy has NO secrets - it only adds authentication.
// The actual secrets are in the external secret-proxy (outside the microVM).
package main

import (
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	tokenPath     = getEnv("TOKEN_PATH", "/var/run/secrets/proxy-auth/token")
	upstreamProxy = getEnv("UPSTREAM_PROXY", "http://secret-proxy.netclode.svc.cluster.local:8080")
	listenAddr    = getEnv("LISTEN_ADDR", ":8080")

	// Token cache (refreshed periodically)
	tokenMu    sync.RWMutex
	tokenCache string
)

func main() {
	// Initial token load
	if err := refreshToken(); err != nil {
		log.Printf("Warning: initial token load failed: %v", err)
	}

	// Refresh token periodically (tokens can be rotated)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			if err := refreshToken(); err != nil {
				log.Printf("Warning: token refresh failed: %v", err)
			}
		}
	}()

	// Create dialer for upstream connections
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	handler := &proxyHandler{
		upstream: upstreamProxy,
		dialer:   dialer,
	}

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	log.Printf("auth-proxy starting on %s, forwarding to %s", listenAddr, upstreamProxy)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func refreshToken() error {
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return err
	}
	tokenMu.Lock()
	tokenCache = strings.TrimSpace(string(data))
	tokenMu.Unlock()
	log.Printf("Token loaded from %s (%d bytes)", tokenPath, len(tokenCache))
	return nil
}

func getToken() string {
	tokenMu.RLock()
	defer tokenMu.RUnlock()
	return tokenCache
}

type proxyHandler struct {
	upstream string
	dialer   *net.Dialer
}

func (h *proxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle CONNECT method (HTTPS tunneling)
	if r.Method == http.MethodConnect {
		h.handleConnect(w, r)
		return
	}

	// Handle regular HTTP requests
	h.handleHTTP(w, r)
}

func (h *proxyHandler) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Create request to upstream proxy
	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			upstreamReq.Header.Add(key, value)
		}
	}

	// Add Proxy-Authorization
	token := getToken()
	if token != "" {
		upstreamReq.Header.Set("Proxy-Authorization", "Bearer "+token)
	}

	// Send via upstream proxy
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(mustParseURL(h.upstream)),
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := client.Do(upstreamReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	io.Copy(w, resp.Body)
}

func (h *proxyHandler) handleConnect(w http.ResponseWriter, r *http.Request) {
	// Connect to upstream proxy
	upstreamReq, err := http.NewRequest(http.MethodConnect, h.upstream, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Set the target host
	upstreamReq.URL.Path = r.Host
	upstreamReq.Host = r.Host

	// Add Proxy-Authorization
	token := getToken()
	if token != "" {
		upstreamReq.Header.Set("Proxy-Authorization", "Bearer "+token)
	}

	// Connect to upstream proxy
	upstreamConn, err := h.dialer.DialContext(r.Context(), "tcp", strings.TrimPrefix(h.upstream, "http://"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Send CONNECT request with auth header
	connectReq := "CONNECT " + r.Host + " HTTP/1.1\r\n"
	connectReq += "Host: " + r.Host + "\r\n"
	if token != "" {
		connectReq += "Proxy-Authorization: Bearer " + token + "\r\n"
	}
	connectReq += "\r\n"

	if _, err := upstreamConn.Write([]byte(connectReq)); err != nil {
		upstreamConn.Close()
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// Read response from upstream
	buf := make([]byte, 1024)
	n, err := upstreamConn.Read(buf)
	if err != nil {
		upstreamConn.Close()
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	response := string(buf[:n])
	if !strings.Contains(response, "200") {
		upstreamConn.Close()
		http.Error(w, "upstream proxy rejected CONNECT: "+response, http.StatusBadGateway)
		return
	}

	// Hijack the client connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstreamConn.Close()
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		upstreamConn.Close()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Send 200 OK to client
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Bidirectional copy
	go func() {
		io.Copy(upstreamConn, clientConn)
		upstreamConn.Close()
	}()
	go func() {
		io.Copy(clientConn, upstreamConn)
		clientConn.Close()
	}()
}

func mustParseURL(rawurl string) *url.URL {
	u, err := url.Parse(rawurl)
	if err != nil {
		panic(err)
	}
	return u
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

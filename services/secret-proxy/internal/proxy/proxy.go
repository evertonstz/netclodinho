// Package proxy implements a MITM proxy that injects secrets into HTTP headers.
//
// The proxy validates each request with the control-plane:
// 1. Extracts Proxy-Authorization header (K8s ServiceAccount token)
// 2. Calls control-plane to validate token and check if target host is allowed
// 3. If allowed, replaces the placeholder in request headers with the real secret
//
// This prevents secret exfiltration because:
// 1. Real secrets never enter the sandbox (they're stored in control-plane)
// 2. Control-plane enforces per-session allowlists based on SDK type
// 3. Replacement only happens in headers, not request bodies
package proxy

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/elazarl/goproxy"
)

// Config holds the proxy configuration.
type Config struct {
	// ListenAddr is the address to listen on (e.g., ":8080").
	ListenAddr string

	// ControlPlaneURL is the URL of the control-plane for auth validation.
	ControlPlaneURL string

	// Secrets maps secret keys to their actual values.
	// e.g., {"anthropic": "sk-ant-...", "openai": "sk-..."}
	Secrets map[string]string

	// CA is the TLS certificate used for MITM.
	CA tls.Certificate

	// Verbose enables verbose logging.
	Verbose bool
}

// validateProxyAuthRequest is the request to control-plane.
type validateProxyAuthRequest struct {
	Token      string `json:"token"`
	TargetHost string `json:"target_host"`
}

// validateProxyAuthResponse is the response from control-plane.
type validateProxyAuthResponse struct {
	Allowed     bool   `json:"allowed"`
	SecretKey   string `json:"secret_key,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Proxy is a MITM proxy that injects secrets into HTTP headers.
type Proxy struct {
	config     Config
	server     *goproxy.ProxyHttpServer
	logger     *slog.Logger
	httpClient *http.Client
}

// connectMeta carries data from the CONNECT request into MITM'd requests.
// This lets us enforce "CONNECT host must match request host".
type connectMeta struct {
	proxyAuth   string
	connectHost string
}

// canonicalHost normalizes a host for policy checks:
// lower-case, no port, no trailing dot, IPv6-safe.
func canonicalHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	host := raw
	if strings.HasPrefix(host, "[") {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		} else {
			host = strings.Trim(host, "[]")
		}
	} else if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	host = strings.TrimSuffix(host, ".")
	return strings.ToLower(host)
}

// New creates a new secret injection proxy.
func New(cfg Config, logger *slog.Logger) *Proxy {
	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = cfg.Verbose

	// Set up custom CA for MITM
	goproxy.GoproxyCa = cfg.CA
	goproxy.OkConnect = &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: goproxy.TLSConfigFromCA(&cfg.CA)}
	goproxy.MitmConnect = &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: goproxy.TLSConfigFromCA(&cfg.CA)}
	goproxy.RejectConnect = &goproxy.ConnectAction{Action: goproxy.ConnectReject, TLSConfig: goproxy.TLSConfigFromCA(&cfg.CA)}

	p := &Proxy{
		config: cfg,
		server: proxy,
		logger: logger,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}

	// Custom CONNECT handler to capture Proxy-Authorization header from CONNECT request
	// This is important because after MITM the request handler sees requests inside
	// the TLS tunnel, which don't have the original CONNECT headers.
	proxy.OnRequest().HandleConnectFunc(func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		// Preserve CONNECT target host and auth for use in handleRequest.
		meta := connectMeta{
			connectHost: canonicalHost(host),
		}
		// Extract Proxy-Authorization from CONNECT request
		if ctx.Req != nil {
			proxyAuth := ctx.Req.Header.Get("Proxy-Authorization")
			if proxyAuth != "" {
				meta.proxyAuth = proxyAuth
				logger.Debug("Captured Proxy-Authorization from CONNECT", "host", host)
			}
		}
		ctx.UserData = meta
		// Continue with MITM
		return goproxy.MitmConnect, host
	})

	// Add request handler for secret injection
	proxy.OnRequest().DoFunc(p.handleRequest)

	return p
}

// handleRequest processes each request and injects secrets where appropriate.
func (p *Proxy) handleRequest(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
	rawHost := req.Host
	if rawHost == "" && req.URL != nil {
		rawHost = req.URL.Host
	}

	scheme := ""
	if req.URL != nil {
		scheme = strings.ToLower(req.URL.Scheme)
	}
	if scheme == "" && req.TLS != nil {
		scheme = "https"
	}
	if scheme != "https" {
		// Never inject secrets into plain HTTP requests.
		p.logger.Debug("Non-HTTPS request, skipping secret injection",
			"host", rawHost,
			"scheme", scheme,
		)
		return req, nil
	}

	urlHost := ""
	if req.URL != nil {
		urlHost = canonicalHost(req.URL.Host)
	}
	reqHost := canonicalHost(req.Host)
	targetHost := urlHost
	if targetHost == "" {
		targetHost = reqHost
	}
	if targetHost == "" {
		p.logger.Debug("Missing host, skipping secret injection")
		return req, nil
	}
	// Guard against Host header spoofing: it must match the actual target host.
	if urlHost != "" && reqHost != "" && urlHost != reqHost {
		p.logger.Warn("Host header mismatch",
			"urlHost", urlHost,
			"reqHost", reqHost,
		)
		return req, goproxy.NewResponse(req, goproxy.ContentTypeText, http.StatusForbidden, "host mismatch")
	}

	// Extract Proxy-Authorization header
	// First try ctx.UserData (captured from CONNECT request for HTTPS)
	// Fall back to request header (for plain HTTP proxying)
	var (
		proxyAuth   string
		connectHost string
	)
	if ctx.UserData != nil {
		switch meta := ctx.UserData.(type) {
		case connectMeta:
			proxyAuth = meta.proxyAuth
			connectHost = meta.connectHost
		case *connectMeta:
			if meta != nil {
				proxyAuth = meta.proxyAuth
				connectHost = meta.connectHost
			}
		case string:
			proxyAuth = meta
		}
	}
	// Enforce that HTTPS requests stay within the CONNECT target host.
	if connectHost != "" && connectHost != targetHost {
		p.logger.Warn("CONNECT host mismatch",
			"connectHost", connectHost,
			"targetHost", targetHost,
		)
		return req, goproxy.NewResponse(req, goproxy.ContentTypeText, http.StatusForbidden, "connect host mismatch")
	}
	if proxyAuth == "" {
		proxyAuth = req.Header.Get("Proxy-Authorization")
	}
	if proxyAuth == "" {
		// No auth header - pass through without injection
		p.logger.Debug("No Proxy-Authorization header, passing through", "host", targetHost)
		return req, nil
	}

	// Remove the "Bearer " prefix
	token := strings.TrimPrefix(proxyAuth, "Bearer ")
	if token == proxyAuth {
		p.logger.Debug("Invalid Proxy-Authorization format (expected Bearer)", "host", targetHost)
		return req, nil
	}

	// Remove Proxy-Authorization header before forwarding (it's for the proxy, not upstream)
	req.Header.Del("Proxy-Authorization")

	// Validate with control-plane using token
	authResult, err := p.validateWithControlPlane(token, targetHost)
	if err != nil {
		p.logger.Warn("Control-plane validation failed", "host", targetHost, "error", err)
		return req, nil // Pass through without injection on error
	}

	if !authResult.Allowed {
		p.logger.Debug("Request not allowed for secret injection",
			"host", targetHost,
			"sessionID", authResult.SessionID,
		)
		return req, nil
	}

	// Get the actual secret value
	secretValue, ok := p.config.Secrets[authResult.SecretKey]
	if !ok {
		p.logger.Warn("Secret key not found in config",
			"secretKey", authResult.SecretKey,
			"host", targetHost,
		)
		return req, nil
	}

	// Replace placeholder in headers ONLY (not in body - prevents reflection attacks)
	injected := false
	for name, values := range req.Header {
		for i, value := range values {
			if strings.Contains(value, authResult.Placeholder) {
				req.Header[name][i] = strings.Replace(value, authResult.Placeholder, secretValue, -1)
				injected = true
			}
		}
	}

	if injected {
		p.logger.Info("Injected secret into request",
			"host", targetHost,
			"secretKey", authResult.SecretKey,
			"sessionID", authResult.SessionID,
		)
	}

	return req, nil
}

// validateWithControlPlane calls the control-plane to validate the proxy auth request.
func (p *Proxy) validateWithControlPlane(token, targetHost string) (*validateProxyAuthResponse, error) {
	reqBody := validateProxyAuthRequest{
		Token:      token,
		TargetHost: targetHost,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimSuffix(p.config.ControlPlaneURL, "/") + "/internal/validate-proxy-auth"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result validateProxyAuthResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, result.Error)
	}

	return &result, nil
}

// ListenAndServe starts the proxy server.
func (p *Proxy) ListenAndServe() error {
	p.logger.Info("Starting secret proxy", "addr", p.config.ListenAddr)
	return http.ListenAndServe(p.config.ListenAddr, p.server)
}

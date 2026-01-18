// Package github provides GitHub App authentication and token generation.
package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/golang-jwt/jwt/v5"
)

// RepoAccess specifies the level of access for repository operations.
type RepoAccess string

const (
	// RepoAccessRead allows read-only access (clone, fetch).
	RepoAccessRead RepoAccess = "read"
	// RepoAccessWrite allows read-write access (clone, push).
	RepoAccessWrite RepoAccess = "write"
)

// Client handles GitHub App authentication and token generation.
type Client struct {
	appID          int64
	installationID int64
	privateKey     *rsa.PrivateKey
	httpClient     *http.Client
}

// NewClient creates a new GitHub client from config.
// Returns nil if GitHub App is not configured.
func NewClient(cfg *config.Config) (*Client, error) {
	if !cfg.HasGitHubApp() {
		return nil, nil
	}

	privateKey, err := parsePrivateKey(cfg.GitHubAppPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("parse GitHub App private key: %w", err)
	}

	return &Client{
		appID:          cfg.GitHubAppID,
		installationID: cfg.GitHubInstallationID,
		privateKey:     privateKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// parsePrivateKey parses a PEM-encoded RSA private key.
// Supports both PKCS#1 and PKCS#8 formats.
func parsePrivateKey(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	// Try PKCS#1 first (GitHub's default format)
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	// Try PKCS#8
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}

	return rsaKey, nil
}

// createJWT creates a JWT for GitHub App authentication.
func (c *Client) createJWT() (string, error) {
	now := time.Now()

	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
		Issuer:    fmt.Sprintf("%d", c.appID),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(c.privateKey)
}

// InstallationToken represents a GitHub installation access token.
type InstallationToken struct {
	Token       string            `json:"token"`
	ExpiresAt   time.Time         `json:"expires_at"`
	Permissions map[string]string `json:"permissions"`
}

// tokenRequest is the request body for creating an installation access token.
type tokenRequest struct {
	Repositories []string          `json:"repositories,omitempty"`
	Permissions  map[string]string `json:"permissions,omitempty"`
}

// CreateInstallationToken creates a scoped installation access token.
// The token can be scoped to specific repositories and permissions.
func (c *Client) CreateInstallationToken(ctx context.Context, repo string, access RepoAccess) (*InstallationToken, error) {
	jwt, err := c.createJWT()
	if err != nil {
		return nil, fmt.Errorf("create JWT: %w", err)
	}

	// Build request body with scoped permissions
	reqBody := tokenRequest{
		Permissions: map[string]string{
			"contents":      string(access),
			"metadata":      "read",
			"actions":       "read",
			"pull_requests": string(access),
			"workflows":     string(access),
		},
	}

	// If repo is specified, scope token to that repository
	if repo != "" {
		// Extract repo name from owner/repo or full URL
		repoName := extractRepoName(repo)
		if repoName != "" {
			reqBody.Repositories = []string{repoName}
		}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%d/access_tokens", c.installationID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		slog.Error("GitHub API error", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("GitHub API error: %s", resp.Status)
	}

	var token InstallationToken
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	slog.Info("Created GitHub installation token",
		"repo", repo,
		"access", access,
		"expiresAt", token.ExpiresAt,
		"permissions", token.Permissions,
	)

	return &token, nil
}

// extractRepoName extracts the repository name from various formats.
// Supports: "owner/repo", "https://github.com/owner/repo", "https://github.com/owner/repo.git"
func extractRepoName(input string) string {
	// Remove .git suffix
	input = strings.TrimSuffix(input, ".git")

	// Handle full URLs
	if strings.HasPrefix(input, "https://github.com/") {
		input = strings.TrimPrefix(input, "https://github.com/")
	} else if strings.HasPrefix(input, "http://github.com/") {
		input = strings.TrimPrefix(input, "http://github.com/")
	}

	// Now we should have "owner/repo"
	parts := strings.Split(input, "/")
	if len(parts) >= 2 {
		// Return just the repo name (not owner/repo)
		return parts[len(parts)-1]
	}

	return ""
}

// NormalizeRepoURL converts various repo formats to a proper HTTPS URL.
// Supports: "owner/repo", "github.com/owner/repo", "https://github.com/owner/repo"
func NormalizeRepoURL(input string) string {
	// Already a full URL with protocol
	if strings.HasPrefix(input, "https://") || strings.HasPrefix(input, "http://") {
		return input
	}

	// Handle github.com/owner/repo (without protocol)
	if strings.HasPrefix(input, "github.com/") {
		return "https://" + input + ".git"
	}

	// Convert owner/repo to HTTPS URL
	if strings.Contains(input, "/") {
		return "https://github.com/" + input + ".git"
	}

	return input
}

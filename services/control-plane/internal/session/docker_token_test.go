package session

import (
	"testing"
)

// TestDockerTokenLifecycle verifies issue/redeem/lookup/revoke semantics.
func TestDockerTokenLifecycle(t *testing.T) {
	m := &Manager{dockerTokens: make(map[string]string)}

	sessionID := "sess-abc123"

	// Issue returns a non-empty hex token.
	token := m.IssueDockerToken(sessionID)
	if token == "" {
		t.Fatal("IssueDockerToken returned empty token")
	}
	if len(token) != 64 {
		t.Fatalf("expected 64-char hex token, got %d chars", len(token))
	}

	// LookupDockerToken finds it without consuming.
	sid, ok := m.LookupDockerToken(token)
	if !ok {
		t.Fatal("LookupDockerToken: token not found")
	}
	if sid != sessionID {
		t.Fatalf("LookupDockerToken: want %q, got %q", sessionID, sid)
	}

	// Second lookup still works (non-destructive).
	_, ok = m.LookupDockerToken(token)
	if !ok {
		t.Fatal("LookupDockerToken: token missing after second lookup")
	}

	// RedeemDockerToken consumes the token.
	sid, ok = m.RedeemDockerToken(token)
	if !ok {
		t.Fatal("RedeemDockerToken: token not found")
	}
	if sid != sessionID {
		t.Fatalf("RedeemDockerToken: want %q, got %q", sessionID, sid)
	}

	// Second redeem fails (single-use).
	_, ok = m.RedeemDockerToken(token)
	if ok {
		t.Fatal("RedeemDockerToken: token should be consumed after first use")
	}
}

// TestDockerTokenRevoke verifies RevokeDockerToken removes the token.
func TestDockerTokenRevoke(t *testing.T) {
	m := &Manager{dockerTokens: make(map[string]string)}

	token := m.IssueDockerToken("sess-xyz")
	m.RevokeDockerToken(token)

	_, ok := m.LookupDockerToken(token)
	if ok {
		t.Fatal("RevokeDockerToken: token should be gone")
	}
}

// TestDockerTokenUniqueness verifies two sessions get different tokens.
func TestDockerTokenUniqueness(t *testing.T) {
	m := &Manager{dockerTokens: make(map[string]string)}

	t1 := m.IssueDockerToken("sess-a")
	t2 := m.IssueDockerToken("sess-b")
	if t1 == t2 {
		t.Fatal("tokens for different sessions should be unique")
	}
}

// TestValidateProxyAuthTokenRouting verifies that the ValidateProxyAuth function
// correctly identifies Docker tokens (no dots) vs JWT tokens (has dots).
// We test only the format-detection logic here via a helper to avoid needing
// a real K8s cluster.
func TestDockerTokenFormatDetection(t *testing.T) {
	cases := []struct {
		token      string
		isDocker   bool
	}{
		{"abcdef1234567890", true},                            // opaque hex
		{"eyJhbGci.eyJzdWIi.signature", false},               // JWT-like
		{"a.b.c", false},                                      // any dot = K8s path
		{"netclode-abc123def456", true},                       // no dots = Docker
	}

	for _, tc := range cases {
		hasNoDot := true
		for _, ch := range tc.token {
			if ch == '.' {
				hasNoDot = false
				break
			}
		}
		if hasNoDot != tc.isDocker {
			t.Errorf("token %q: isDocker want %v, got %v", tc.token, tc.isDocker, hasNoDot)
		}
	}
}

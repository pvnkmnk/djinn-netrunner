package api

import (
	"testing"
)

func TestGenerateOAuthState_Random(t *testing.T) {
	// Generate multiple states and verify they are unique
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		state, err := generateOAuthState()
		if err != nil {
			t.Fatalf("generateOAuthState() returned error: %v", err)
		}
		if len(state) != 64 {
			t.Errorf("expected state length 64 (32 bytes hex-encoded), got %d", len(state))
		}
		if seen[state] {
			t.Errorf("duplicate state generated: %s", state)
		}
		seen[state] = true
	}
}

func TestGenerateOAuthState_Format(t *testing.T) {
	state, err := generateOAuthState()
	if err != nil {
		t.Fatalf("generateOAuthState() returned error: %v", err)
	}

	// Verify it's valid hex
	if len(state) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(state))
	}

	for _, c := range state {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("invalid hex character: %c", c)
		}
	}
}

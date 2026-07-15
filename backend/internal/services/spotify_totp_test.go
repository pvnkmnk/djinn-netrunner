package services

import (
	"testing"
)

func TestDeriveSpotifySecret(t *testing.T) {
	// The derived secret should be deterministic
	s1 := deriveSpotifySecret()
	s2 := deriveSpotifySecret()

	if len(s1) == 0 {
		t.Fatal("derived secret is empty")
	}
	if len(s1) != len(s2) {
		t.Fatalf("derived secret length mismatch: %d vs %d", len(s1), len(s2))
	}
	for i := range s1 {
		if s1[i] != s2[i] {
			t.Fatalf("derived secret differs at byte %d", i)
		}
	}
}

func TestGenerateSpotifyTOTP(t *testing.T) {
	// TOTP should always be 6 digits
	code := generateSpotifyTOTP(1700000000)
	if len(code) != 6 {
		t.Errorf("expected 6-digit TOTP, got %q (len=%d)", code, len(code))
	}

	// Verify it's all digits
	for _, c := range code {
		if c < '0' || c > '9' {
			t.Errorf("TOTP contains non-digit character: %c", c)
		}
	}

	// Same timestamp should produce same code
	code2 := generateSpotifyTOTP(1700000000)
	if code != code2 {
		t.Errorf("same timestamp produced different codes: %q vs %q", code, code2)
	}

	// Different 30-second windows should produce different codes (very high probability)
	code3 := generateSpotifyTOTP(1700000000 + 30)
	if code == code3 {
		// This could theoretically happen but is extremely unlikely
		t.Logf("warning: adjacent time windows produced same code %q (unlikely but possible)", code)
	}
}

func TestHexStringToBytes(t *testing.T) {
	tests := []struct {
		input    string
		expected []byte
	}{
		{"", nil},
		{"00", []byte{0}},
		{"ff", []byte{255}},
		{"0a0b", []byte{10, 11}},
		{"48656c6c6f", []byte("Hello")},
	}

	for _, tc := range tests {
		got := hexStringToBytes(tc.input)
		if len(tc.expected) == 0 && len(got) == 0 {
			continue
		}
		if len(got) != len(tc.expected) {
			t.Errorf("hexStringToBytes(%q): got len %d, want %d", tc.input, len(got), len(tc.expected))
			continue
		}
		for i := range got {
			if got[i] != tc.expected[i] {
				t.Errorf("hexStringToBytes(%q): byte %d = %d, want %d", tc.input, i, got[i], tc.expected[i])
			}
		}
	}
}

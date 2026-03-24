package services

import (
	"net"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"127.0.0.1", true},
		{"169.254.1.1", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"fc00::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"142.250.80.46", false}, // google.com
	}

	for _, tt := range tests {
		ip := net.ParseIP(tt.ip)
		if ip == nil {
			t.Fatalf("failed to parse IP: %s", tt.ip)
		}
		got := isPrivateIP(ip)
		if got != tt.private {
			t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

func TestSafeGet_RejectsPrivateIP(t *testing.T) {
	// localhost should be blocked
	_, err := SafeGet("http://localhost:8080/test")
	if err == nil {
		t.Error("expected error for localhost URL, got nil")
	}

	_, err = SafeGet("http://127.0.0.1:8080/test")
	if err == nil {
		t.Error("expected error for 127.0.0.1 URL, got nil")
	}

	_, err = SafeGet("http://10.0.0.1/test")
	if err == nil {
		t.Error("expected error for 10.0.0.1 URL, got nil")
	}
}

func TestSafeGet_RejectsInvalidScheme(t *testing.T) {
	_, err := SafeGet("file:///etc/passwd")
	if err == nil {
		t.Error("expected error for file:// scheme, got nil")
	}
}

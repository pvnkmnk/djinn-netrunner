package services

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Enable loopback for tests to allow httptest.NewServer to work with SSRF protection
	allowLoopback = true
	code := m.Run()
	os.Exit(code)
}

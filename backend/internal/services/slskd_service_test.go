package services

import (
	"testing"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
)

func TestSlskdService(t *testing.T) {
	cfg := &config.Config{
		SlskdURL: "http://localhost:5030",
	}
	s := NewSlskdService(cfg)

	if s == nil {
		t.Fatal("Expected SlskdService to be initialized")
	}
}

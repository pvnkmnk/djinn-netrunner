package services

import (
	"testing"
)

func TestGonicClient(t *testing.T) {
	c := NewGonicClient("http://localhost:4747", "admin", "admin", nil)
	if c == nil {
		t.Fatal("Expected GonicClient to be initialized")
	}
}

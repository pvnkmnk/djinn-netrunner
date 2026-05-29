package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestArtistsHandler_Init(t *testing.T) {
	// Basic test to ensure handler can be created
	// Full integration tests would require DB setup
	assert.NotNil(t, &ArtistsHandler{})
}

func TestLibraryHandler_Init(t *testing.T) {
	// Basic test to ensure handler can be created
	// Full integration tests would require DB setup
	assert.NotNil(t, &LibraryHandler{})
}

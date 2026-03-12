package main

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestCLIInitialization(t *testing.T) {
	// Verify that subcommands are correctly defined
	assert.NotNil(t, statusCmd())
	assert.NotNil(t, configCmd())
	assert.NotNil(t, watchlistCmd())
}

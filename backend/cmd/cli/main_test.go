package main

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestCLIInitialization(t *testing.T) {
	// Verify that subcommands are correctly defined
	assert.NotNil(t, statusCmd())
	assert.NotNil(t, configCmd())

	wCmd := watchlistCmd()
	assert.NotNil(t, wCmd)

	// Check for import subcommand
	found := false
	for _, sub := range wCmd.Commands() {
		if sub.Name() == "import" {
			found = true
			break
		}
	}
	assert.True(t, found, "import subcommand should exist")
}

package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiteFSGuard_IsPrimary(t *testing.T) {
	// Create a temp directory to simulate LiteFS mount point
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir", "netrunner.db")

	// Ensure subdir exists
	err := os.MkdirAll(filepath.Dir(dbPath), 0755)
	require.NoError(t, err)

	guard := NewLiteFSGuard(dbPath)

	// Case 1: No .primary file (not using LiteFS) -> should be primary
	assert.True(t, guard.IsPrimary(), "Should be primary when no .primary file exists")

	// Case 2: Empty .primary file -> should be primary
	// LiteFSGuard looks for .primary in filepath.Dir(dbPath) = tmpDir/subdir
	mountDir := filepath.Dir(dbPath)
	primaryPath := filepath.Join(mountDir, ".primary")
	err = os.WriteFile(primaryPath, []byte(""), 0644)
	require.NoError(t, err)

	guard2 := NewLiteFSGuard(dbPath)
	assert.True(t, guard2.IsPrimary(), "Should be primary when .primary file is empty")

	// Case 3: .primary file contains our hostname -> should be primary
	hostname, err := os.Hostname()
	require.NoError(t, err)
	err = os.WriteFile(primaryPath, []byte(hostname), 0644)
	require.NoError(t, err)

	guard3 := NewLiteFSGuard(dbPath)
	assert.True(t, guard3.IsPrimary(), "Should be primary when .primary contains our hostname")

	// Case 4: .primary file contains different hostname -> should not be primary
	err = os.WriteFile(primaryPath, []byte("other-hostname"), 0644)
	require.NoError(t, err)

	guard4 := NewLiteFSGuard(dbPath)
	assert.False(t, guard4.IsPrimary(), "Should not be primary when .primary contains different hostname")
}

func TestLiteFSGuard_GetPrimaryHostname(t *testing.T) {
	// Create a temp directory
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir", "netrunner.db")

	// Ensure subdir exists
	err := os.MkdirAll(filepath.Dir(dbPath), 0755)
	require.NoError(t, err)

	// Case 1: No .primary file -> empty string
	guard := NewLiteFSGuard(dbPath)
	hostname := guard.GetPrimaryHostname()
	assert.Equal(t, "", hostname, "Should return empty string when no .primary file")

	// Case 2: .primary file exists with hostname
	// LiteFSGuard looks for .primary in filepath.Dir(dbPath) = tmpDir/subdir
	mountDir := filepath.Dir(dbPath)
	primaryPath := filepath.Join(mountDir, ".primary")
	expectedHostname := "test-primary-host"
	err = os.WriteFile(primaryPath, []byte(expectedHostname), 0644)
	require.NoError(t, err)

	guard2 := NewLiteFSGuard(dbPath)
	hostname2 := guard2.GetPrimaryHostname()
	assert.Equal(t, expectedHostname, hostname2)
}

func TestLiteFSGuard_IsPrimary_FileReadError(t *testing.T) {
	// Test that IsPrimary returns true when ReadFile fails on .primary.
	// Create the .primary path as a directory so os.ReadFile gets "is a directory" error.

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "subdir", "netrunner.db")

	// Ensure subdir exists
	err := os.MkdirAll(filepath.Dir(dbPath), 0755)
	require.NoError(t, err)

	// Create .primary as a directory (not a file) to force os.ReadFile error
	mountDir := filepath.Dir(dbPath)
	primaryPath := filepath.Join(mountDir, ".primary")
	err = os.MkdirAll(primaryPath, 0755)
	require.NoError(t, err)

	guard := NewLiteFSGuard(dbPath)
	// ReadFile on a directory returns an error -> IsPrimary returns true (fallback)
	assert.True(t, guard.IsPrimary())
}

package database

import (
	"os"
	"path/filepath"
	"strings"
)

// LiteFSGuard handles primary node detection for LiteFS
type LiteFSGuard struct {
	dbPath string
}

// NewLiteFSGuard creates a new LiteFS guard
func NewLiteFSGuard(dbPath string) *LiteFSGuard {
	return &LiteFSGuard{dbPath: dbPath}
}

// IsPrimary returns true if the current node is the primary LiteFS node
func (g *LiteFSGuard) IsPrimary() bool {
	// 1. If not running LiteFS, we are effectively the primary
	// LiteFS usually mounts at a specific path, and the DB is inside it.
	// The .primary file is in the root of the mount.
	mountDir := filepath.Dir(g.dbPath)
	primaryPath := filepath.Join(mountDir, ".primary")

	data, err := os.ReadFile(primaryPath)
	if err != nil {
		// If the file doesn't exist, we assume we are primary (or not using LiteFS)
		return true
	}

	// 2. If the file is empty, we are primary
	content := strings.TrimSpace(string(data))
	if content == "" {
		return true
	}

	// 3. Otherwise, the file contains the hostname of the primary
	// If it matches our hostname, we are primary
	hostname, _ := os.Hostname()
	return content == hostname
}

// GetPrimaryHostname returns the hostname of the primary node
func (g *LiteFSGuard) GetPrimaryHostname() string {
	mountDir := filepath.Dir(g.dbPath)
	primaryPath := filepath.Join(mountDir, ".primary")

	data, err := os.ReadFile(primaryPath)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(data))
}

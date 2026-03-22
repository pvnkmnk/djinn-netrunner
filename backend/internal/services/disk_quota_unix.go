//go:build !windows
// +build !windows

package services

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// getFilesystemUsage returns total and free bytes for the filesystem containing path (Unix).
func getFilesystemUsage(path string) (total, free int64, err error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, 0, fmt.Errorf("statfs failed: %w", err)
	}
	total = int64(stat.Bsize) * int64(stat.Blocks)
	free = int64(stat.Bsize) * int64(stat.Bfree)
	return total, free, nil
}

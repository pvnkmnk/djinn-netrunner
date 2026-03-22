//go:build windows
// +build windows

package services

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// getFilesystemUsage returns total and free bytes for the filesystem containing path (Windows).
func getFilesystemUsage(path string) (total, free int64, err error) {
	var freeBytes, totalBytes, totalFree uint64
	err = windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(path), &freeBytes, &totalBytes, &totalFree)
	if err != nil {
		return 0, 0, fmt.Errorf("GetDiskFreeSpaceEx failed: %v", err)
	}
	return int64(totalBytes), int64(freeBytes), nil
}

//go:build windows

package hoststats

import "golang.org/x/sys/windows"

// diskFreeBytes returns free (available to the calling user) and total
// bytes for the volume containing path. Mirrors
// internal/app/files/diskspace_windows.go's localDiskFreeBytes.
func diskFreeBytes(path string) (free, total int64, err error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeAvail, totalBytes, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &freeAvail, &totalBytes, &totalFree); err != nil {
		return 0, 0, err
	}
	return int64(freeAvail), int64(totalBytes), nil
}

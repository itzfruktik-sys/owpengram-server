//go:build !windows

package hoststats

import "golang.org/x/sys/unix"

// diskFreeBytes returns free (available to an unprivileged caller, not
// counting reserved blocks) and total bytes for the filesystem containing
// path. Mirrors internal/app/files/diskspace_unix.go's localDiskFreeBytes --
// duplicated locally rather than exported cross-package since this is a
// three-line syscall wrapper, not shared logic worth coupling two packages
// over.
func diskFreeBytes(path string) (free, total int64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), int64(st.Blocks) * int64(st.Bsize), nil
}

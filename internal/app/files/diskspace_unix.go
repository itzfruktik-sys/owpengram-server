//go:build !windows

package files

import "golang.org/x/sys/unix"

// localDiskFreeBytes returns free (available to an unprivileged writer, not
// counting reserved blocks) and total bytes for the filesystem containing path.
func localDiskFreeBytes(path string) (free, total int64, err error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), int64(st.Blocks) * int64(st.Bsize), nil
}

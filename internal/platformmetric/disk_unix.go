//go:build !windows

package platformmetric

import "golang.org/x/sys/unix"

// ReadDisk reads the filesystem holding dir: its size and the bytes free on
// it, both in the filesystem's own block size.
func ReadDisk(dir string) (Disk, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return Disk{}, err
	}
	bs := uint64(st.Bsize)
	return Disk{TotalBytes: uint64(st.Blocks) * bs, FreeBytes: uint64(st.Bfree) * bs}, nil
}

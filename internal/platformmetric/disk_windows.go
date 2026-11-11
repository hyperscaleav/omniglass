//go:build windows

package platformmetric

import "golang.org/x/sys/windows"

// ReadDisk reads the volume holding dir: its size and the bytes free on it.
func ReadDisk(dir string) (Disk, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return Disk{}, err
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return Disk{}, err
	}
	return Disk{TotalBytes: total, FreeBytes: free}, nil
}

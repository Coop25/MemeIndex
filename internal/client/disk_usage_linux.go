//go:build linux

package client

import "syscall"

// diskUsage reports the space on the filesystem holding path. The second return
// value is false when the probe could not read the filesystem.
func diskUsage(path string) (adminDiskStats, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return adminDiskStats{Path: path}, false
	}
	bsize := uint64(st.Bsize)
	stats := adminDiskStats{
		Path:       path,
		TotalBytes: st.Blocks * bsize,
		FreeBytes:  st.Bavail * bsize,
		UsedBytes:  (st.Blocks - st.Bfree) * bsize,
	}
	stats.Low = diskIsLow(stats.TotalBytes, stats.FreeBytes)
	return stats, true
}

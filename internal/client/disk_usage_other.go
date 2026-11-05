//go:build !linux

package client

// diskUsage is not implemented off Linux. It always reports the probe as failed.
func diskUsage(path string) (adminDiskStats, bool) {
	return adminDiskStats{Path: path}, false
}

package client

// diskLowFloorBytes is the absolute free-space floor below which a data volume
// is reported as low, regardless of its percentage free.
const diskLowFloorBytes = 25 << 30

// adminDiskStats describes the space on the volume that holds the data directory.
type adminDiskStats struct {
	Path       string `json:"path"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	Low        bool   `json:"low"`
}

// diskIsLow reports whether free space is critically low: less than 10% of the
// volume or less than diskLowFloorBytes. A zero-sized volume is never low, since
// it means the probe could not read real numbers.
func diskIsLow(total, free uint64) bool {
	if total == 0 {
		return false
	}
	return free < total/10 || free < diskLowFloorBytes
}

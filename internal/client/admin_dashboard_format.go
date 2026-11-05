package client

import (
	"fmt"
	"time"
)

// formatAdminBytes renders a byte count with a binary unit, e.g. "1.5 GB".
func formatAdminBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, exp := float64(n), 0
	for value >= unit && exp < 5 {
		value /= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", value, "KMGTPE"[exp-1])
}

// formatAdminAge renders how long ago something happened: "today",
// "1 day ago", "3 days ago".
func formatAdminAge(d time.Duration) string {
	days := int(d / (24 * time.Hour))
	switch {
	case days <= 0:
		return "today"
	case days == 1:
		return "1 day ago"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}

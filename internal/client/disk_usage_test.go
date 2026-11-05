package client

import "testing"

func TestDiskIsLow(t *testing.T) {
	const gib = uint64(1) << 30
	tests := []struct {
		name  string
		total uint64
		free  uint64
		want  bool
	}{
		{"5 percent free on 1 TiB is low", 1000 * gib, 50 * gib, true},
		{"15 percent free on 1 TiB is not low", 1000 * gib, 150 * gib, false},
		{"under 25 GiB floor on 100 GiB volume is low", 100 * gib, 20 * gib, true},
		{"zero total is never low", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diskIsLow(tt.total, tt.free); got != tt.want {
				t.Fatalf("diskIsLow(%d, %d) = %v, want %v", tt.total, tt.free, got, tt.want)
			}
		})
	}
}

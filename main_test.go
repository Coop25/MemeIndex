package main

import (
	"testing"
	"time"
)

func TestDurationUntilNextUTCMidnight(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Duration
	}{
		{
			name: "two hours before midnight UTC",
			now:  time.Date(2026, 3, 4, 22, 0, 0, 0, time.UTC),
			want: 2 * time.Hour,
		},
		{
			name: "exactly midnight UTC is a full day away",
			now:  time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC),
			want: 24 * time.Hour,
		},
		{
			name: "input in a non-UTC zone is converted first",
			// 03:00 at UTC+5 is 22:00 UTC the previous day.
			now:  time.Date(2026, 3, 4, 3, 0, 0, 0, time.FixedZone("plus5", 5*3600)),
			want: 2 * time.Hour,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := durationUntilNextUTCMidnight(tc.now); got != tc.want {
				t.Fatalf("durationUntilNextUTCMidnight(%s) = %s, want %s", tc.now, got, tc.want)
			}
		})
	}
}

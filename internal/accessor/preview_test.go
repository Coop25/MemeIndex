package accessor

import (
	"strconv"
	"testing"
)

func TestEvenlySpacedVideoOffsetsCoversClip(t *testing.T) {
	offsets := evenlySpacedVideoOffsets(10, 3)
	if len(offsets) != 3 || offsets[0] != "0.000" {
		t.Fatalf("unexpected offsets %v", offsets)
	}
	middle, err := strconv.ParseFloat(offsets[1], 64)
	if err != nil || middle < 4 || middle > 5 {
		t.Fatalf("middle frame did not sample the middle of the clip: %v", offsets)
	}
	last, err := strconv.ParseFloat(offsets[2], 64)
	if err != nil || last < 8 || last >= 10 {
		t.Fatalf("last frame did not sample near the end of the clip: %v", offsets)
	}
}

func TestEvenlySpacedVideoOffsetsSingleFrameStartsAtBeginning(t *testing.T) {
	offsets := evenlySpacedVideoOffsets(15, 1)
	if len(offsets) != 1 || offsets[0] != "0" {
		t.Fatalf("unexpected offsets %v", offsets)
	}
}

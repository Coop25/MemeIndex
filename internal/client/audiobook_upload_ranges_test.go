package client

import (
	"reflect"
	"testing"
)

func TestMergeUploadRanges(t *testing.T) {
	cases := []struct {
		name  string
		start []uploadByteRange
		add   uploadByteRange
		want  []uploadByteRange
	}{
		{
			name:  "first range",
			start: nil,
			add:   uploadByteRange{Start: 0, End: 10},
			want:  []uploadByteRange{{Start: 0, End: 10}},
		},
		{
			name:  "disjoint ranges stay separate",
			start: []uploadByteRange{{Start: 0, End: 10}},
			add:   uploadByteRange{Start: 20, End: 30},
			want:  []uploadByteRange{{Start: 0, End: 10}, {Start: 20, End: 30}},
		},
		{
			name:  "out of order insert sorts",
			start: []uploadByteRange{{Start: 20, End: 30}},
			add:   uploadByteRange{Start: 0, End: 10},
			want:  []uploadByteRange{{Start: 0, End: 10}, {Start: 20, End: 30}},
		},
		{
			name:  "adjacent ranges merge",
			start: []uploadByteRange{{Start: 0, End: 10}},
			add:   uploadByteRange{Start: 10, End: 20},
			want:  []uploadByteRange{{Start: 0, End: 20}},
		},
		{
			name:  "overlapping ranges merge",
			start: []uploadByteRange{{Start: 0, End: 10}},
			add:   uploadByteRange{Start: 5, End: 15},
			want:  []uploadByteRange{{Start: 0, End: 15}},
		},
		{
			name:  "re-adding the same range is idempotent",
			start: []uploadByteRange{{Start: 0, End: 10}},
			add:   uploadByteRange{Start: 0, End: 10},
			want:  []uploadByteRange{{Start: 0, End: 10}},
		},
		{
			name:  "range fills a gap between two others and merges them into one",
			start: []uploadByteRange{{Start: 0, End: 10}, {Start: 20, End: 30}},
			add:   uploadByteRange{Start: 10, End: 20},
			want:  []uploadByteRange{{Start: 0, End: 30}},
		},
		{
			name:  "range spans and absorbs an existing smaller range",
			start: []uploadByteRange{{Start: 10, End: 15}},
			add:   uploadByteRange{Start: 0, End: 30},
			want:  []uploadByteRange{{Start: 0, End: 30}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeUploadRanges(tc.start, tc.add.Start, tc.add.End)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("mergeUploadRanges(%v, %d, %d) = %v, want %v", tc.start, tc.add.Start, tc.add.End, got, tc.want)
			}
		})
	}
}

func TestMergeUploadRangesConcurrentChunksAssembleWholeFile(t *testing.T) {
	// Simulates four concurrent 10-byte chunk streams for a 40-byte upload
	// landing in a scrambled order - the kind of interleaving several
	// parallel upload connections would actually produce.
	var ranges []uploadByteRange
	order := []uploadByteRange{
		{Start: 20, End: 30},
		{Start: 0, End: 10},
		{Start: 30, End: 40},
		{Start: 10, End: 20},
	}
	for _, r := range order {
		ranges = mergeUploadRanges(ranges, r.Start, r.End)
	}
	want := []uploadByteRange{{Start: 0, End: 40}}
	if !reflect.DeepEqual(ranges, want) {
		t.Fatalf("merged ranges = %v, want %v", ranges, want)
	}
	if got := sumUploadRanges(ranges); got != 40 {
		t.Fatalf("sumUploadRanges = %d, want 40", got)
	}
}

func TestSumUploadRanges(t *testing.T) {
	ranges := []uploadByteRange{{Start: 0, End: 10}, {Start: 20, End: 35}}
	if got := sumUploadRanges(ranges); got != 25 {
		t.Fatalf("sumUploadRanges = %d, want 25", got)
	}
	if got := sumUploadRanges(nil); got != 0 {
		t.Fatalf("sumUploadRanges(nil) = %d, want 0", got)
	}
}

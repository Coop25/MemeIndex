package audiobooks

import (
	"reflect"
	"testing"
)

func TestMergeUploadRanges(t *testing.T) {
	cases := []struct {
		name  string
		start []ByteRange
		add   ByteRange
		want  []ByteRange
	}{
		{
			name:  "first range",
			start: nil,
			add:   ByteRange{Start: 0, End: 10},
			want:  []ByteRange{{Start: 0, End: 10}},
		},
		{
			name:  "disjoint ranges stay separate",
			start: []ByteRange{{Start: 0, End: 10}},
			add:   ByteRange{Start: 20, End: 30},
			want:  []ByteRange{{Start: 0, End: 10}, {Start: 20, End: 30}},
		},
		{
			name:  "out of order insert sorts",
			start: []ByteRange{{Start: 20, End: 30}},
			add:   ByteRange{Start: 0, End: 10},
			want:  []ByteRange{{Start: 0, End: 10}, {Start: 20, End: 30}},
		},
		{
			name:  "adjacent ranges merge",
			start: []ByteRange{{Start: 0, End: 10}},
			add:   ByteRange{Start: 10, End: 20},
			want:  []ByteRange{{Start: 0, End: 20}},
		},
		{
			name:  "overlapping ranges merge",
			start: []ByteRange{{Start: 0, End: 10}},
			add:   ByteRange{Start: 5, End: 15},
			want:  []ByteRange{{Start: 0, End: 15}},
		},
		{
			name:  "re-adding the same range is idempotent",
			start: []ByteRange{{Start: 0, End: 10}},
			add:   ByteRange{Start: 0, End: 10},
			want:  []ByteRange{{Start: 0, End: 10}},
		},
		{
			name:  "range fills a gap between two others and merges them into one",
			start: []ByteRange{{Start: 0, End: 10}, {Start: 20, End: 30}},
			add:   ByteRange{Start: 10, End: 20},
			want:  []ByteRange{{Start: 0, End: 30}},
		},
		{
			name:  "range spans and absorbs an existing smaller range",
			start: []ByteRange{{Start: 10, End: 15}},
			add:   ByteRange{Start: 0, End: 30},
			want:  []ByteRange{{Start: 0, End: 30}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MergeUploadRanges(tc.start, tc.add.Start, tc.add.End)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("MergeUploadRanges(%v, %d, %d) = %v, want %v", tc.start, tc.add.Start, tc.add.End, got, tc.want)
			}
		})
	}
}

func TestMergeUploadRangesConcurrentChunksAssembleWholeFile(t *testing.T) {
	// Simulates four concurrent 10-byte chunk streams for a 40-byte upload
	// landing in a scrambled order - the kind of interleaving several
	// parallel upload connections would actually produce.
	var ranges []ByteRange
	order := []ByteRange{
		{Start: 20, End: 30},
		{Start: 0, End: 10},
		{Start: 30, End: 40},
		{Start: 10, End: 20},
	}
	for _, r := range order {
		ranges = MergeUploadRanges(ranges, r.Start, r.End)
	}
	want := []ByteRange{{Start: 0, End: 40}}
	if !reflect.DeepEqual(ranges, want) {
		t.Fatalf("merged ranges = %v, want %v", ranges, want)
	}
	if got := SumUploadRanges(ranges); got != 40 {
		t.Fatalf("SumUploadRanges = %d, want 40", got)
	}
}

func TestSumUploadRanges(t *testing.T) {
	ranges := []ByteRange{{Start: 0, End: 10}, {Start: 20, End: 35}}
	if got := SumUploadRanges(ranges); got != 25 {
		t.Fatalf("SumUploadRanges = %d, want 25", got)
	}
	if got := SumUploadRanges(nil); got != 0 {
		t.Fatalf("SumUploadRanges(nil) = %d, want 0", got)
	}
}

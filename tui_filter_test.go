package main

import (
	"reflect"
	"testing"
)

func TestMergeIndices(t *testing.T) {
	tests := []struct {
		name     string
		existing []int
		newIdx   []int
		want     []int
	}{
		{
			name:     "no overlap",
			existing: []int{1, 3},
			newIdx:   []int{2, 4},
			want:     []int{1, 2, 3, 4},
		},
		{
			name:     "overlap",
			existing: []int{1, 2, 3},
			newIdx:   []int{2, 3, 4},
			want:     []int{1, 2, 3, 4},
		},
		{
			name:     "unsorted new",
			existing: []int{1, 5},
			newIdx:   []int{4, 2},
			want:     []int{1, 2, 4, 5},
		},
		{
			name:     "empty existing",
			existing: []int{},
			newIdx:   []int{2, 1},
			want:     []int{1, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeIndices(tt.existing, tt.newIdx)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mergeIndices() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFuzzyFilter(t *testing.T) {
	targets := []string{
		"sum.golang.org\x01sumdb\x01https://sum.golang.org/",
		"log2025-1.rekor.sigstore.dev\x01tiles\x01https://log2025-1.rekor.sigstore.dev/api/v2/",
		"transparency.dev/armored-witness/firmware_transparency/prod/1\x01serverless\x01https://api.transparency.dev/armored-witness-firmware/prod/log/1/",
	}

	// 1. Empty query
	t.Run("empty query", func(t *testing.T) {
		got := fuzzyFilter("", targets)
		if len(got) != len(targets) {
			t.Fatalf("expected %d ranks, got %d", len(targets), len(got))
		}
		for i := range targets {
			if got[i].Index != i {
				t.Errorf("expected index %d, got %d", i, got[i].Index)
			}
		}
	})

	// 2. AND Logic (all terms must match)
	t.Run("AND logic", func(t *testing.T) {
		// "rekor" matches rekor, "tiles" matches rekor and sumdb? No, tiles matches only rekor
		got := fuzzyFilter("rekor tiles", targets)
		if len(got) != 1 {
			t.Fatalf("expected 1 match, got %d", len(got))
		}
		if got[0].Index != 1 {
			t.Errorf("expected index 1, got %d", got[0].Index)
		}
	})

	t.Run("AND logic no match", func(t *testing.T) {
		got := fuzzyFilter("rekor sumdb", targets)
		if len(got) != 0 {
			t.Fatalf("expected 0 matches, got %d", len(got))
		}
	})

	// 3. Order independence
	t.Run("order independence", func(t *testing.T) {
		got1 := fuzzyFilter("rekor tiles", targets)
		got2 := fuzzyFilter("tiles rekor", targets)
		if !reflect.DeepEqual(got1, got2) {
			t.Errorf("expected order independence: %v vs %v", got1, got2)
		}
	})

	// 4. Index filtering (only match before the first \x01)
	t.Run("index filtering", func(t *testing.T) {
		// "sumdb" is after \x01 in target 0. It should match the target but its MatchedIndexes must NOT contain indices >= limit (which is 14)
		got := fuzzyFilter("sumdb", targets)
		if len(got) != 1 {
			t.Fatalf("expected 1 match, got %d", len(got))
		}
		if got[0].Index != 0 {
			t.Fatalf("expected index 0, got %d", got[0].Index)
		}
		for _, idx := range got[0].MatchedIndexes {
			if idx >= 14 {
				t.Errorf("matched index %d exceeds the limit 14", idx)
			}
		}
	})

	t.Run("index filtering with mixed matches", func(t *testing.T) {
		got := fuzzyFilter("sum.golang.org sumdb", targets)
		if len(got) != 1 {
			t.Fatalf("expected 1 match, got %d", len(got))
		}
		if got[0].Index != 0 {
			t.Fatalf("expected index 0, got %d", got[0].Index)
		}
		if len(got[0].MatchedIndexes) == 0 {
			t.Errorf("expected some matched indexes, got none")
		}
		for _, idx := range got[0].MatchedIndexes {
			if idx >= 14 {
				t.Errorf("matched index %d exceeds the limit 14", idx)
			}
		}
	})

	t.Run("case insensitivity", func(t *testing.T) {
		got := fuzzyFilter("SUM.GOLANG", targets)
		if len(got) != 1 {
			t.Fatalf("expected 1 match, got %d", len(got))
		}
		if got[0].Index != 0 {
			t.Errorf("expected index 0, got %d", got[0].Index)
		}
	})

	t.Run("no match", func(t *testing.T) {
		got := fuzzyFilter("nonexistent", targets)
		if len(got) != 0 {
			t.Errorf("expected 0 matches, got %d", len(got))
		}
	})

	t.Run("partial match some targets", func(t *testing.T) {
		got := fuzzyFilter("rekor", targets)
		if len(got) != 1 {
			t.Fatalf("expected 1 match, got %d", len(got))
		}
		if got[0].Index != 1 {
			t.Errorf("expected index 1, got %d", got[0].Index)
		}
	})
}

package main

import "testing"

func TestEdgeInsertIndexYieldsWholeCardAfterTop(t *testing.T) {
	// Three 100px cards with 8px gaps: tops at 0, 108, 216.
	heights := []float32{100, 100, 100}
	gap := float32(8)

	cases := []struct {
		name string
		y    float32
		want int
	}{
		{"above first", -1, 0},
		{"just into first top", 0, 0},
		{"mid first (midpoint would still be 0; edge keeps 0)", 50, 0},
		{"near first bottom", 99, 0},
		{"past first bottom into gap", 100, 1},
		{"into second top", 108, 1},
		{"mid second", 158, 1},
		{"past second", 208, 2},
		{"into third", 216, 2},
		{"below last", 400, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := edgeInsertIndex(tc.y, heights, gap)
			if got != tc.want {
				t.Fatalf("y=%.0f: got %d want %d", tc.y, got, tc.want)
			}
		})
	}
}

func TestEdgeInsertIndexOldTopCheckWasInverted(t *testing.T) {
	// Document the bug: old `if y < cursor { break }` treated any y on the
	// card body as "after", so mid-card never yielded.
	heights := []float32{100, 100}
	gap := float32(8)
	y := float32(40) // over first card
	got := edgeInsertIndex(y, heights, gap)
	if got != 0 {
		t.Fatalf("hovering first card must insert before it (yield), got %d", got)
	}
	// Old algorithm would have returned 1 here.
}

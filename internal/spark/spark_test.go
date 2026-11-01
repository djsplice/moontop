package spark

import (
	"math"
	"testing"
)

func TestLine(t *testing.T) {
	// A ramp over 8 samples in 4 characters: heights 1..4 left to right.
	got := Line([]float64{0, 0, 1, 1, 2, 2, 3, 3}, 8, 4, 0, 3, 1)
	if got != "⣀⣤⣶⣿" {
		t.Fatalf("ramp = %q", got)
	}
	// Fewer samples than the window: the missing start is blank.
	if got := Line([]float64{5, 5}, 8, 4, 0, 10, 1); got != "   ⣶" { // 5 of 10: 3 dots
		t.Fatalf("partial = %q", got)
	}
	// A flat line sits on the bottom row.
	if got := Line([]float64{25, 25, 25, 25}, 4, 2, 25, 25, 5); got != "⣀⣀" {
		t.Fatalf("flat = %q", got)
	}
	// Averaging: 8 samples into 2 columns (one character).
	// left column low (1 dot), right column high (4 dots)
	if got := Line([]float64{0, 0, 0, 0, 3, 3, 3, 3}, 8, 1, 0, 3, 1); got != "⣸" {
		t.Fatalf("averaged = %q", got)
	}
	if Line(nil, 10, 0, 0, 1, 1) != "" {
		t.Fatal("zero width")
	}
	if got := Line([]float64{math.NaN(), math.NaN()}, 2, 1, 0, 1, 1); got != " " {
		t.Fatalf("NaN = %q", got)
	}
}

func TestRangeAndRing(t *testing.T) {
	r := NewRing(3)
	r.Add(1, 2, 3, 4)
	r.Add(5)
	if s := r.Samples(); len(s) != 3 || s[0] != 3 || s[2] != 5 {
		t.Fatalf("ring %v", s)
	}
	if lo, hi, ok := Range([]float64{9, 1, 5, math.NaN()}, 10); !ok || lo != 1 || hi != 9 {
		t.Fatalf("range %v %v %v", lo, hi, ok)
	}
	if _, _, ok := Range(nil, 10); ok {
		t.Fatal("empty range ok")
	}
}

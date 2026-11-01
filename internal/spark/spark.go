// Package spark draws one-line braille sparklines: each character is a
// braille cell of 2 × 4 dots, so it holds two samples at four heights.
// Suggested by djsplice on the Moontop Discord ("Need a little braille
// sparkline history for nozzle/bed temps").
package spark

import "math"

// Dot bits per column, bottom row first (Unicode braille, U+2800 base).
var (
	leftDots  = [4]rune{0x40, 0x04, 0x02, 0x01}
	rightDots = [4]rune{0x80, 0x20, 0x10, 0x08}
)

// Line renders the last samples (oldest first) as width characters
// covering window samples: each of the 2·width columns is the average
// of its share of the window. Samples missing from the start of the
// window (not recorded yet) are blank. Heights run from lo to hi (hi is
// raised to at least lo+minSpan so a flat line stays at the bottom);
// every recorded column shows at least one dot.
func Line(samples []float64, window, width int, lo, hi, minSpan float64) string {
	if width <= 0 {
		return ""
	}
	cols := 2 * width
	if window < cols {
		window = cols
	}
	if len(samples) > window {
		samples = samples[len(samples)-window:]
	}
	missing := window - len(samples) // at the start
	if hi < lo+minSpan {
		hi = lo + minSpan
	}
	level := make([]int, cols) // 0 = no data, 1..4 dots
	for c := 0; c < cols; c++ {
		from, to := c*window/cols, (c+1)*window/cols
		sum, n := 0.0, 0
		for i := from; i < to; i++ {
			if j := i - missing; j >= 0 && j < len(samples) && !math.IsNaN(samples[j]) {
				sum += samples[j]
				n++
			}
		}
		if n == 0 {
			continue
		}
		f := (sum/float64(n) - lo) / (hi - lo)
		level[c] = 1 + int(math.Round(math.Max(0, math.Min(1, f))*3))
	}
	out := make([]rune, width)
	for i := range out {
		l, r := level[2*i], level[2*i+1]
		if l == 0 && r == 0 {
			out[i] = ' '
			continue
		}
		cell := rune(0x2800)
		for d := 0; d < l; d++ {
			cell |= leftDots[d]
		}
		for d := 0; d < r; d++ {
			cell |= rightDots[d]
		}
		out[i] = cell
	}
	return string(out)
}

// Range is the low and high of samples' last window (NaNs ignored);
// ok=false when there are none.
func Range(samples []float64, window int) (lo, hi float64, ok bool) {
	if len(samples) > window {
		samples = samples[len(samples)-window:]
	}
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, v := range samples {
		if math.IsNaN(v) {
			continue
		}
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	return lo, hi, !math.IsInf(lo, 1)
}

// Ring keeps the most recent samples, at most cap.
type Ring struct {
	cap  int
	data []float64
}

func NewRing(cap int) *Ring { return &Ring{cap: cap} }

// Add appends samples, dropping the oldest beyond the capacity.
func (r *Ring) Add(v ...float64) {
	r.data = append(r.data, v...)
	if over := len(r.data) - r.cap; over > 0 {
		r.data = append(r.data[:0:0], r.data[over:]...)
	}
}

// Samples returns the samples, oldest first.
func (r *Ring) Samples() []float64 { return r.data }

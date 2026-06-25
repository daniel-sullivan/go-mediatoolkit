//go:build cgo

package window

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/flac/internal/nativeflac"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goWindow runs the nativeflac port for the given WindowType into a
// fresh slice, mirroring the parameter routing of ApplyWindow.
func goWindow(typ int, L int32, p, start, end float32) []float32 {
	out := make([]float32, L)
	nativeflac.ApplyWindow(out, L, nativeflac.WindowType(typ), p, start, end)
	return out
}

// assertBitExact compares two float32 slices by IEEE-754 bit pattern so
// that -0.0 vs +0.0 and NaN payloads are caught (a plain == would treat
// NaN!=NaN and merge signed zeros).
func assertBitExact(t *testing.T, c, g []float32) {
	t.Helper()
	require.Equal(t, len(c), len(g), "length mismatch")
	for i := range c {
		cb := math.Float32bits(c[i])
		gb := math.Float32bits(g[i])
		assert.Equalf(t, cb, gb,
			"index %d: C=%v (0x%08x) Go=%v (0x%08x)", i, c[i], cb, g[i], gb)
	}
}

// blockSizes spans odd/even small sizes (exercising the L&1 branches in
// bartlett/triangle), powers of two, and typical FLAC block sizes.
var blockSizes = []int32{16, 17, 31, 32, 64, 256, 512, 1024, 4096, 4097, 8192}

// noParamWindows covers the generators that ignore p/start/end.
var noParamWindows = []struct {
	name string
	typ  int
}{
	{"bartlett", 0},
	{"bartlett_hann", 1},
	{"blackman", 2},
	{"blackman_harris_4term_92db", 3},
	{"connes", 4},
	{"flattop", 5},
	{"hamming", 7},
	{"hann", 8},
	{"kaiser_bessel", 9},
	{"nuttall", 10},
	{"rectangle", 11},
	{"triangle", 12},
	{"welch", 16},
}

func TestWindowNoParamParity(t *testing.T) {
	for _, w := range noParamWindows {
		for _, L := range blockSizes {
			t.Run(w.name, func(t *testing.T) {
				c := CWindow(w.typ, L, 0, 0, 0)
				g := goWindow(w.typ, L, 0, 0, 0)
				assertBitExact(t, c, g)
			})
		}
	}
}

func TestWindowGaussParity(t *testing.T) {
	// In-range stddev values plus out-of-range / NaN to exercise the
	// recursive default-to-0.25 branch.
	stddevs := []float32{0.1, 0.25, 0.4, 0.5, 0.6, -0.1, 0, float32(math.NaN())}
	for _, sd := range stddevs {
		for _, L := range blockSizes {
			t.Run("gauss", func(t *testing.T) {
				c := CWindow(6, L, sd, 0, 0)
				g := goWindow(6, L, sd, 0, 0)
				assertBitExact(t, c, g)
			})
		}
	}
}

func TestWindowTukeyParity(t *testing.T) {
	ps := []float32{0.1, 0.25, 0.5, 0.75, 0.9, 0, 1.0, -0.5, 1.5, float32(math.NaN())}
	for _, p := range ps {
		for _, L := range blockSizes {
			t.Run("tukey", func(t *testing.T) {
				c := CWindow(13, L, p, 0, 0)
				g := goWindow(13, L, p, 0, 0)
				assertBitExact(t, c, g)
			})
		}
	}
}

func TestWindowPartialPunchoutTukeyParity(t *testing.T) {
	type pc struct{ p, start, end float32 }
	cases := []pc{
		{0.5, 0.0, 0.5},
		{0.5, 0.25, 0.75},
		{0.2, 0.1, 0.9},
		{0.95, 0.3, 0.6},
		{0.0, 0.0, 0.5},   // -> p=0.05 recursion
		{1.0, 0.25, 0.75}, // -> p=0.95 recursion
		{float32(math.NaN()), 0.2, 0.8},
	}
	for _, typ := range []int{14, 15} { // partial_tukey, punchout_tukey
		for _, c := range cases {
			for _, L := range blockSizes {
				t.Run("multi_tukey", func(t *testing.T) {
					cc := CWindow(typ, L, c.p, c.start, c.end)
					gg := goWindow(typ, L, c.p, c.start, c.end)
					assertBitExact(t, cc, gg)
				})
			}
		}
	}
}

// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package psymodel

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// requireStrict skips a parity test unless the mp3_strict build tag is set.
// The FHT butterflies are floating-point (each term is a separately rounded
// c*x + s*y), so the result is only a bit-exact target under the FMA-free
// strict build against the -ffp-contract=off cgo oracle. A bare `go test`
// therefore stays clean; `mise run //libraries/mp3:parity` (which sets
// -tags=mp3_strict + the scalar CGO flags) is the single bit-exact gate. See
// the FP-parity convention in CONTRIBUTING.md.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("psychoacoustic-model parity asserts FP bit-exactness; run under -tags=mp3_strict (mise run //libraries/mp3:parity)")
	}
}

// f32bitsEqual asserts two float32 slices are bit-identical via their raw
// IEEE-754 bit patterns (NaN-safe), the bit-exactness contract for the FHT
// spectrum. ctx is a short label identifying the failing case.
func f32bitsEqual(t *testing.T, want, got []float32, ctx string) {
	t.Helper()
	require.Equal(t, len(want), len(got))
	for i := range want {
		wb, gb := math.Float32bits(want[i]), math.Float32bits(got[i])
		if wb != gb {
			assert.Failf(t, "fht bit mismatch",
				"%s: out[%d] c=%v(0x%08x) go=%v(0x%08x)", ctx, i, want[i], wb, got[i], gb)
			return
		}
	}
}

// fhtCase names an FHT size to exercise: bufLen is the number of FLOATs the
// transform operates on in place, and n is the argument fht doubles internally
// (n = bufLen/2, matching fft_long's fht(x, BLKSIZE/2) and fft_short's
// fht(x, BLKSIZE_s/2)).
type fhtCase struct {
	name   string
	bufLen int
	n      int
}

// fhtCases covers both FFT sizes the psychoacoustic model uses: the long-block
// FFT (BLKSIZE = 1024) and the short-block FFT (BLKSIZE_s = 256). The fht
// argument is bufLen/2 in both, since fht doubles n internally ("to get
// BLKSIZE, because of 3DNow! ASM routine", fft.c:70).
var fhtCases = []fhtCase{
	{"long_BLKSIZE_1024", nativemp3.BLKSIZE, nativemp3.BLKSIZE / 2},
	{"short_BLKSIZEs_256", nativemp3.BLKSIZEs, nativemp3.BLKSIZEs / 2},
}

// genInputs builds a spread of input buffers per size that exercises the FHT's
// rounding: small spectra, large-magnitude spectra, impulse/edge spectra (to
// hit the sin/cos extremes of the trig recurrence) and many uniform-random
// fills. All values are finite float32 so the bit-exact comparison is
// meaningful (NaN/Inf propagation is identical on both sides but uninformative).
func genInputs(rng *rand.Rand, bufLen int) [][]float32 {
	var inputs [][]float32

	// Several scales of uniform random fill — the bulk of the corpus.
	for _, scale := range []float32{1e-6, 1.0, 1e3, 1e6} {
		for iter := 0; iter < 64; iter++ {
			b := make([]float32, bufLen)
			for i := range b {
				b[i] = (rng.Float32()*2 - 1) * scale
			}
			inputs = append(inputs, b)
		}
	}

	// Impulses at a few positions — concentrates energy so the butterfly
	// products span the full magnitude range of the trig recurrence.
	for _, pos := range []int{0, 1, bufLen / 4, bufLen / 2, bufLen - 1} {
		b := make([]float32, bufLen)
		b[pos] = 1.0
		inputs = append(inputs, b)
		b2 := make([]float32, bufLen)
		b2[pos] = -3.7e5
		inputs = append(inputs, b2)
	}

	// All-ones and an alternating square wave (DC and Nyquist extremes).
	ones := make([]float32, bufLen)
	alt := make([]float32, bufLen)
	for i := range ones {
		ones[i] = 1.0
		if i%2 == 0 {
			alt[i] = 1.0
		} else {
			alt[i] = -1.0
		}
	}
	inputs = append(inputs, ones, alt)

	return inputs
}

// TestFhtParity is the core psychoacoustic-model parity assertion. For both FFT
// sizes the model uses (long BLKSIZE, short BLKSIZE_s) it runs the vendored C
// fht and the Go nativemp3.Fht over identical input buffers and asserts the
// in-place transformed spectra are bit-for-bit equal.
func TestFhtParity(t *testing.T) {
	requireStrict(t)

	rng := rand.New(rand.NewSource(0x9501da))
	total := 0
	for _, tc := range fhtCases {
		for idx, in := range genInputs(rng, tc.bufLen) {
			cOut := cgoFht(in, tc.n)
			gOut := goFht(in, tc.n)
			f32bitsEqual(t, cOut, gOut, fmt.Sprintf("%s case=%d", tc.name, idx))
			total++
		}
	}
	require.Greater(t, total, 100, "corpus too small — FHT parity is not being exercised")
}

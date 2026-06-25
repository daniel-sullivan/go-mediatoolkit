//go:build cgo

package fixed_encode

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// buildData allocates a buffer of FLAC__MAX_FIXED_ORDER (=4) history
// samples plus dataLen signal samples, filled with random int32 values
// in [-half, half). `bits` selects the magnitude so the order-4
// finite-difference (worst-case multiplier 6, plus the order-1..4 cross
// terms) stays inside int32, keeping the C narrow accumulator and the
// Go uint32 accumulator on the same overflow page.
func buildData(r *rand.Rand, dataLen int, bits int) []int32 {
	out := make([]int32, 4+dataLen)
	half := int32(1) << (bits - 1)
	for i := range out {
		out[i] = int32(r.Uint32()&uint32(2*half-1)) - half
	}
	return out
}

// assertBitsEqual compares two bits[] estimate arrays. The estimate is a
// float32 produced by an identical double-precision log chain in both
// implementations, so equality must be exact (NaN-aware).
func assertBitsEqual(t *testing.T, want, got [5]float32, ctx string) {
	t.Helper()
	for i := range want {
		w, g := want[i], got[i]
		if math.IsNaN(float64(w)) && math.IsNaN(float64(g)) {
			continue
		}
		assert.Equal(t, math.Float32bits(w), math.Float32bits(g),
			"%s bits[%d] want=%v got=%v", ctx, i, w, g)
	}
}

func TestParityBestPredictor(t *testing.T) {
	r := rand.New(rand.NewPCG(0xF1, 0xED))
	// dataLen 0 exercises the all-zero-error branch (bits[*]==0, order 0).
	dataLens := []int{0, 1, 2, 5, 16, 256, 4096}
	// 18-bit signal: at order 4 the residual is bounded by
	// (1+4+6+4+1)*2^17 = 16*2^17 = 2^21, well inside int32, and the
	// 32-bit accumulator over <=4096 samples stays < 2^33... but the C
	// narrow path uses uint32, so keep dataLen*peak under 2^32. With
	// 18-bit data, order-0 peak ~2^17, 4096*2^17 = 2^29 < 2^32. OK.
	for _, dataLen := range dataLens {
		for _, bits := range []int{4, 12, 18} {
			data := buildData(r, dataLen, bits)
			cOrder, cBits := cgoBestPredictor(data, uint32(dataLen))
			var gBits [5]float32
			gOrder := nativeflac.FixedComputeBestPredictor(data, uint32(dataLen), &gBits)
			require.Equal(t, cOrder, gOrder,
				"order mismatch dataLen=%d bits=%d", dataLen, bits)
			assertBitsEqual(t, cBits, gBits,
				"BestPredictor dataLen="+itoa(dataLen)+" bits="+itoa(bits))
		}
	}
}

func TestParityBestPredictorWide(t *testing.T) {
	r := rand.New(rand.NewPCG(0xC0, 0xDE))
	dataLens := []int{0, 1, 2, 5, 16, 256, 4096, 16384}
	// Wide path uses uint64 accumulators, so larger bit depths are safe;
	// the per-difference value must still fit int32 before local_abs
	// (the C computes the differences in int32 in both narrow and wide
	// variants), so cap the data magnitude so order-4 stays in int32:
	// 16*2^(bits-1) < 2^31 => bits <= 27. Use up to 26.
	for _, dataLen := range dataLens {
		for _, bits := range []int{8, 20, 26} {
			data := buildData(r, dataLen, bits)
			cOrder, cBits := cgoBestPredictorWide(data, uint32(dataLen))
			var gBits [5]float32
			gOrder := nativeflac.FixedComputeBestPredictorWide(data, uint32(dataLen), &gBits)
			require.Equal(t, cOrder, gOrder,
				"order mismatch dataLen=%d bits=%d", dataLen, bits)
			assertBitsEqual(t, cBits, gBits,
				"BestPredictorWide dataLen="+itoa(dataLen)+" bits="+itoa(bits))
		}
	}
}

// TestParityBestPredictorTieBreaking drives constant and ramp signals
// where multiple orders produce equal error sums, exercising the
// "prefer lower order" <= tie-breaking ladder (fixed.c:262) exactly.
func TestParityBestPredictorTieBreaking(t *testing.T) {
	cases := map[string][]int32{
		// all-equal -> every difference order >=1 is zero; order 0 also
		// nonzero unless value is 0. Constant nonzero: orders 1..4 all 0,
		// so order 1 wins via the ladder.
		"constant":  {7, 7, 7, 7, 7, 7, 7, 7},
		"zeros":     {0, 0, 0, 0, 0, 0, 0, 0},
		"linear":    {0, 1, 2, 3, 4, 5, 6, 7},     // order2+ -> 0
		"quadratic": {0, 1, 4, 9, 16, 25, 36, 49}, // order3+ -> 0
	}
	for name, signal := range cases {
		// Prepend 4 history samples that continue the pattern backwards
		// so the residual at the block start is well-defined; use zeros
		// here and rely on libFLAC reading the same history.
		data := make([]int32, 4+len(signal))
		copy(data[4:], signal)
		dataLen := uint32(len(signal))
		cOrder, cBits := cgoBestPredictor(data, dataLen)
		var gBits [5]float32
		gOrder := nativeflac.FixedComputeBestPredictor(data, dataLen, &gBits)
		require.Equal(t, cOrder, gOrder, "tie order mismatch case=%s", name)
		assertBitsEqual(t, cBits, gBits, "tie case="+name)
	}
}

// itoa is a tiny local int->string helper to keep test messages cheap
// without pulling strconv into the assertion path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

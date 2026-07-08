//go:build cgo

package fvad_sp

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/fvad"
)

// makeInt16 builds n deterministic PRNG int16 samples with the extremes
// (±32767, -32768) and zero runs injected — GetScalingSquare's negation
// of -32768 and the saturating paths only fire on extreme inputs.
func makeInt16(rng *rand.Rand, n int) []int16 {
	out := make([]int16, n)
	for i := range out {
		switch rng.IntN(20) {
		case 0:
			out[i] = -32768
		case 1:
			out[i] = 32767
		case 2:
			out[i] = 0
		default:
			out[i] = int16(rng.IntN(65536) - 32768)
		}
	}
	return out
}

// makeInt32 builds n deterministic PRNG int32 samples spanning the
// magnitudes the intermediate resampler stages carry (Q15-shifted int16
// range and beyond), plus extremes.
func makeInt32(rng *rand.Rand, n int) []int32 {
	out := make([]int32, n)
	for i := range out {
		switch rng.IntN(20) {
		case 0:
			out[i] = math.MinInt32 / 2
		case 1:
			out[i] = math.MaxInt32 / 2
		case 2:
			out[i] = 0
		default:
			// Q15-shifted int16 with offset — the documented domain.
			out[i] = (int32(rng.IntN(65536)-32768) << 15) + (1 << 14)
		}
	}
	return out
}

// interestingU32 spans every leading-zeros class plus neighbours.
func interestingU32() []uint32 {
	vals := []uint32{0, 1, 2, 3, 0x7FFFFFFF, 0x80000000, 0xFFFFFFFF}
	for s := 0; s < 32; s++ {
		v := uint32(1) << s
		vals = append(vals, v, v-1, v+1)
	}
	return vals
}

func TestNormAndSizeInBitsParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	cases := interestingU32()
	for i := 0; i < 100000; i++ {
		cases = append(cases, rng.Uint32())
	}
	for _, u := range cases {
		require.Equal(t, cGetSizeInBits(u), fvad.GetSizeInBits(u), "GetSizeInBits(%#x)", u)
		require.Equal(t, cNormU32(u), fvad.NormU32(u), "NormU32(%#x)", u)
		a := int32(u)
		require.Equal(t, cNormW32(a), fvad.NormW32(a), "NormW32(%d)", a)
		// Pin the table-driven C fallback too — it is what non-GNU
		// builds of the oracle use, and it must agree with the Go
		// (math/bits) implementation as well. clz == 32 − GetSizeInBits.
		require.Equal(t, cCountLeadingZeros32NotBuiltin(u), int(32-fvad.GetSizeInBits(u)),
			"CountLeadingZeros32(%#x)", u)
	}
}

func TestDivW32W16Parity(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	nums := []int32{0, 1, -1, 131072, math.MaxInt32, math.MinInt32 + 1, math.MinInt32}
	dens := []int16{0, 1, -1, 2, -2, 384, -384, 10000, 32767, -32768}
	for i := 0; i < 2000; i++ {
		nums = append(nums, int32(rng.Uint32()))
		dens = append(dens, int16(rng.IntN(65536)-32768))
	}
	for _, num := range nums {
		for _, den := range dens {
			if num == math.MinInt32 && den == -1 {
				// INT32_MIN / -1 overflows — UB (a trap on the oracle
				// side, a panic on the Go side). No libfvad call site
				// can produce this pair: every numerator is either a
				// bounded positive expression (131072 + std/2) or the
				// |·| of a >>4-shifted value, which cannot be INT32_MIN.
				continue
			}
			require.Equal(t, cDivW32W16(num, den), fvad.DivW32W16(num, den),
				"DivW32W16(%d, %d)", num, den)
		}
	}
}

func TestGetScalingSquareAndEnergyParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	lengths := []int{1, 2, 3, 60, 80, 120, 160, 240}
	for _, n := range lengths {
		for rep := 0; rep < 50; rep++ {
			buf := makeInt16(rng, n)
			require.Equal(t, cGetScalingSquare(buf, n), fvad.GetScalingSquare(buf, n),
				"GetScalingSquare len=%d rep=%d", n, rep)
			cEn, cScale := cEnergy(buf)
			gEn, gScale := fvad.Energy(buf)
			require.Equal(t, cEn, gEn, "Energy value len=%d rep=%d", n, rep)
			require.Equal(t, cScale, gScale, "Energy scale len=%d rep=%d", n, rep)
		}
	}

	// Degenerate buffers: all zero (smax == 0 branch) and all -32768
	// (the negation-wrap quirk leaves smax at -1).
	for _, n := range []int{1, 80, 240} {
		zero := make([]int16, n)
		require.Equal(t, cGetScalingSquare(zero, n), fvad.GetScalingSquare(zero, n), "all-zero len=%d", n)
		cEn, cScale := cEnergy(zero)
		gEn, gScale := fvad.Energy(zero)
		require.Equal(t, cEn, gEn)
		require.Equal(t, cScale, gScale)

		ext := make([]int16, n)
		for i := range ext {
			ext[i] = -32768
		}
		require.Equal(t, cGetScalingSquare(ext, n), fvad.GetScalingSquare(ext, n), "all-min len=%d", n)
		cEn, cScale = cEnergy(ext)
		gEn, gScale = fvad.Energy(ext)
		require.Equal(t, cEn, gEn, "all-min energy len=%d", n)
		require.Equal(t, cScale, gScale, "all-min scale len=%d", n)
	}
}

func TestDownsamplingParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	// Chunked stream with carried filter state, at each valid frame
	// length the 16/32 kHz paths feed it.
	for _, chunk := range []int{160, 320, 480, 640, 960} {
		cState := make([]int32, 2)
		gState := make([]int32, 2)
		for rep := 0; rep < 40; rep++ {
			in := makeInt16(rng, chunk)
			cOut := make([]int16, chunk/2)
			gOut := make([]int16, chunk/2)
			cDownsampling(in, cOut, cState, chunk)
			fvad.Downsampling(in, gOut, gState, chunk)
			require.Equal(t, cOut, gOut, "Downsampling output chunk=%d rep=%d", chunk, rep)
			require.Equal(t, cState, gState, "Downsampling state chunk=%d rep=%d", chunk, rep)
		}
	}
}

func TestDownBy2ShortToIntParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 10))
	cState := make([]int32, 8)
	gState := make([]int32, 8)
	for rep := 0; rep < 60; rep++ {
		in := makeInt16(rng, 480)
		cOut := make([]int32, 240)
		gOut := make([]int32, 240)
		cDownBy2ShortToInt(in, 480, cOut, cState)
		fvad.DownBy2ShortToInt(in, 480, gOut, gState)
		require.Equal(t, cOut, gOut, "DownBy2ShortToInt output rep=%d", rep)
		require.Equal(t, cState, gState, "DownBy2ShortToInt state rep=%d", rep)
	}
}

func TestDownBy2IntToShortParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(11, 12))
	cState := make([]int32, 8)
	gState := make([]int32, 8)
	for rep := 0; rep < 60; rep++ {
		in := makeInt32(rng, 160)
		cIn := append([]int32(nil), in...) // the function scribbles on in
		gIn := append([]int32(nil), in...)
		cOut := make([]int16, 80)
		gOut := make([]int16, 80)
		cDownBy2IntToShort(cIn, 160, cOut, cState)
		fvad.DownBy2IntToShort(gIn, 160, gOut, gState)
		require.Equal(t, cOut, gOut, "DownBy2IntToShort output rep=%d", rep)
		require.Equal(t, cState, gState, "DownBy2IntToShort state rep=%d", rep)
		require.Equal(t, cIn, gIn, "DownBy2IntToShort scratch rep=%d", rep)
	}
}

func TestLPBy2IntToIntParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(13, 14))
	cState := make([]int32, 16)
	gState := make([]int32, 16)
	for rep := 0; rep < 60; rep++ {
		in := makeInt32(rng, 240)
		cOut := make([]int32, 240)
		gOut := make([]int32, 240)
		cLPBy2IntToInt(in, 240, cOut, cState)
		fvad.LPBy2IntToInt(in, 240, gOut, gState)
		require.Equal(t, cOut, gOut, "LPBy2IntToInt output rep=%d", rep)
		require.Equal(t, cState, gState, "LPBy2IntToInt state rep=%d", rep)
	}
}

func TestResample48khzTo32khzParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(15, 16))
	for _, k := range []int{1, 4, 80} {
		for rep := 0; rep < 30; rep++ {
			in := makeInt32(rng, 3*k+6)
			cOut := make([]int32, 2*k)
			gOut := make([]int32, 2*k)
			cResample48khzTo32khz(in, cOut, k)
			fvad.Resample48khzTo32khz(in, gOut, k)
			require.Equal(t, cOut, gOut, "Resample48khzTo32khz k=%d rep=%d", k, rep)
		}
	}
}

func TestResample48khzTo8khzParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(17, 18))
	cState := newCState48To8()
	var gState fvad.State48khzTo8khz
	for rep := 0; rep < 100; rep++ {
		in := makeInt16(rng, 480)
		cOut := make([]int16, 80)
		gOut := make([]int16, 80)
		cTmp := make([]int32, 480+256) // zeroed, like CalcVad48khz's = {0}
		gTmp := make([]int32, 480+256)
		cState.resample(in, cOut, cTmp)
		fvad.Resample48khzTo8khz(in, gOut, &gState, gTmp)
		require.Equal(t, cOut, gOut, "Resample48khzTo8khz output rep=%d", rep)

		s4824, s2424, s2416, s168 := cState.snapshot()
		require.Equal(t, s4824, gState.S48To24[:], "S_48_24 rep=%d", rep)
		require.Equal(t, s2424, gState.S24To24[:], "S_24_24 rep=%d", rep)
		require.Equal(t, s2416, gState.S24To16[:], "S_24_16 rep=%d", rep)
		require.Equal(t, s168, gState.S16To8[:], "S_16_8 rep=%d", rep)
	}
}

func TestFindMinimumParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(19, 20))

	// Feature values span the realistic Q4 log-energy range plus
	// extremes and negatives; long runs age values past the 100-frame
	// window so the remove-and-shift path fires.
	makeValue := func() int16 {
		switch rng.IntN(10) {
		case 0:
			return -32768
		case 1:
			return 32767
		case 2:
			return 0
		case 3:
			return 10000 // equals the empty-slot sentinel
		default:
			return int16(rng.IntN(4000))
		}
	}

	cInst := newCFindMinimumInst()
	gInst := new(fvad.Inst)
	gInst.InitCore()

	for frame := 0; frame < 500; frame++ {
		// FindMinimum reads frame_counter but GmmProbability owns
		// incrementing it; drive the counter externally through the
		// values that flip its branches (0, 1..2, >2).
		fc := int32(frame)
		cInst.setFrameCounter(fc)
		gInst.FrameCounter = fc

		for channel := 0; channel < 6; channel++ {
			v := makeValue()
			cMed := cInst.findMinimum(v, channel)
			gMed := fvad.FindMinimum(gInst, v, channel)
			require.Equal(t, cMed, gMed, "median frame=%d ch=%d value=%d", frame, channel, v)
		}

		lowValues, ages, means := cInst.snapshot()
		require.Equal(t, lowValues[:], gInst.LowValueVector[:], "low_value_vector frame=%d", frame)
		require.Equal(t, ages[:], gInst.IndexVector[:], "index_vector frame=%d", frame)
		require.Equal(t, means[:], gInst.MeanValue[:], "mean_value frame=%d", frame)
	}
}

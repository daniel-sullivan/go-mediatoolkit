//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_CeltFloat2Int16_SIMDvsScalar cross-checks the NEON
// FCVTAS path against the scalar math.RoundToEven path. They differ
// by ≤1 ULP on exact half-integer inputs (ties-away vs ties-to-even)
// and otherwise match bit-exactly; the tolerance is 1 int16 ULP.
func TestParity_CeltFloat2Int16_SIMDvsScalar(t *testing.T) {
	if !nativeopus.ExportTestFloat2Int16SIMDAvailable() {
		t.Skip("float2int16 SIMD not compiled in (opus_strict/opus_nosimd/non-arm64)")
	}
	counts := []int{0, 1, 8, 15, 16, 17, 32, 64, 240, 960}
	for _, seed := range []int64{42, 101, 777} {
		r := rand.New(rand.NewSource(seed))
		for _, cnt := range counts {
			// Draw float32s in [-2, 2) so we cover the full [-32768, 65535]
			// pre-saturation range and exercise both clamps.
			in := make([]float32, cnt)
			for i := range in {
				in[i] = r.Float32()*4 - 2
			}
			scalarIn := append([]float32(nil), in...)
			simdIn := append([]float32(nil), in...)

			scalarOut := make([]int16, cnt)
			simdOut := make([]int16, cnt)

			nativeopus.ExportTestCeltFloat2Int16Scalar(scalarIn, scalarOut, cnt)
			if cnt >= 16 {
				nativeopus.ExportTestCeltFloat2Int16SIMD(simdIn, simdOut, cnt)
			} else {
				// Below dispatch threshold — scalar is the de-facto SIMD path.
				nativeopus.ExportTestCeltFloat2Int16Scalar(simdIn, simdOut, cnt)
			}

			for i := 0; i < cnt; i++ {
				diff := int(scalarOut[i]) - int(simdOut[i])
				if diff < -1 || diff > 1 {
					t.Fatalf("seed=%d cnt=%d i=%d in=%g scalar=%d simd=%d diff=%d",
						seed, cnt, i, in[i], scalarOut[i], simdOut[i], diff)
				}
			}
		}
	}
}

// TestParity_OpusLimit2CheckWithin1_SIMDvsScalar compares the NEON
// clip+checkwithin1 kernel against a slower reference that also
// tracks exceeding1. The arrays must match bit-exactly (the op is
// clip-to-[-2,2], no FP rounding difference), and the SIMD must
// agree with the reference on whether any sample was outside [-1,1].
func TestParity_OpusLimit2CheckWithin1_SIMDvsScalar(t *testing.T) {
	if !nativeopus.ExportTestLimit2SIMDAvailable() {
		t.Skip("limit2 SIMD not compiled in")
	}
	counts := []int{0, 1, 8, 15, 16, 17, 32, 64, 240, 960}
	for _, seed := range []int64{42, 101, 777} {
		r := rand.New(rand.NewSource(seed))
		for _, cnt := range counts {
			// Mix ranges: most in [-0.8, 0.8], a few in (1, 2], some in (2, 3],
			// some in [-3, -2). This exercises all return-value branches.
			in := make([]float32, cnt)
			for i := range in {
				switch r.Intn(4) {
				case 0:
					in[i] = r.Float32()*1.6 - 0.8
				case 1:
					in[i] = 1.0 + r.Float32() // (1, 2]
					if r.Intn(2) == 0 {
						in[i] = -in[i]
					}
				case 2:
					in[i] = 2.0 + r.Float32() // (2, 3]
					if r.Intn(2) == 0 {
						in[i] = -in[i]
					}
				case 3:
					in[i] = -2.5 - r.Float32() // [-3.5, -2.5]
				}
			}

			// Reference: scalar clip + exceeding1 tracking.
			refIn := append([]float32(nil), in...)
			refExceeding := 0
			for i := 0; i < cnt; i++ {
				v := refIn[i]
				if v > 1.0 || v < -1.0 {
					refExceeding = 1
				}
				if v < -2.0 {
					v = -2.0
				}
				if v > 2.0 {
					v = 2.0
				}
				refIn[i] = v
			}
			refRet := 1 - refExceeding

			// SIMD under test (only if cnt>=16, else just use scalar).
			simdIn := append([]float32(nil), in...)
			var simdRet int
			if cnt >= 16 {
				simdRet = int(nativeopus.ExportTestOpusLimit2CheckWithin1SIMD(simdIn, cnt))
			} else {
				// Below threshold: exercise the dispatch (which stays scalar).
				simdRet = nativeopus.ExportTestOpusLimit2CheckWithin1C(simdIn, cnt)
				// The scalar path returns 0 as "unknown" when cnt > 0, or 1
				// when cnt == 0. Normalise reference to match that below.
				if cnt > 0 {
					refRet = 0
				}
			}

			// Clipped-array comparison: must match bit-exactly.
			for i := 0; i < cnt; i++ {
				if refIn[i] != simdIn[i] {
					t.Fatalf("seed=%d cnt=%d i=%d orig=%g ref=%g simd=%g",
						seed, cnt, i, in[i], refIn[i], simdIn[i])
				}
			}
			// Return-value comparison.
			if simdRet != refRet {
				t.Fatalf("seed=%d cnt=%d: ref=%d simd=%d", seed, cnt, refRet, simdRet)
			}
		}
	}
}

// Benchmarks: 20 ms @ 48 kHz = 960 samples, the typical opus frame.

func benchFloat2Int16Input(n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(0.9 * math.Sin(2*math.Pi*440*float64(i)/48000))
	}
	return out
}

func BenchmarkCeltFloat2Int16_Scalar_960(b *testing.B) {
	in := benchFloat2Int16Input(960)
	out := make([]int16, 960)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nativeopus.ExportTestCeltFloat2Int16Scalar(in, out, 960)
	}
}

func BenchmarkCeltFloat2Int16_SIMD_960(b *testing.B) {
	if !nativeopus.ExportTestFloat2Int16SIMDAvailable() {
		b.Skip("float2int16 SIMD not compiled in")
	}
	in := benchFloat2Int16Input(960)
	out := make([]int16, 960)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nativeopus.ExportTestCeltFloat2Int16SIMD(in, out, 960)
	}
}

func benchLimit2Input(n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		// Peak ≈ 1.5 so some samples exceed [-1,1] but none exceed [-2,2].
		out[i] = float32(1.5 * math.Sin(2*math.Pi*440*float64(i)/48000))
	}
	return out
}

func BenchmarkOpusLimit2CheckWithin1_Scalar_960(b *testing.B) {
	base := benchLimit2Input(960)
	work := make([]float32, 960)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(work, base)
		nativeopus.ExportTestOpusLimit2CheckWithin1Scalar(work, 960)
	}
}

func BenchmarkOpusLimit2CheckWithin1_SIMD_960(b *testing.B) {
	if !nativeopus.ExportTestLimit2SIMDAvailable() {
		b.Skip("limit2 SIMD not compiled in")
	}
	base := benchLimit2Input(960)
	work := make([]float32, 960)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(work, base)
		nativeopus.ExportTestOpusLimit2CheckWithin1SIMD(work, 960)
	}
}

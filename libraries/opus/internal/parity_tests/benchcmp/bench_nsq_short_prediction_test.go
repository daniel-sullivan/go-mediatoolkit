//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// BenchmarkNSQShortPrediction_Scalar4x measures four serial scalar
// calls to silk_noise_shape_quantizer_short_prediction_c — the cost
// the current del_dec inner loop pays at nStates=4, complexity 10.
func BenchmarkNSQShortPrediction_Scalar4x(b *testing.B) {
	r := rand.New(rand.NewSource(1))
	const order = 16
	const base = 30
	const maxLanes = 4
	aos := make([]nativeopus.NSQ_del_dec_struct, maxLanes)
	for k := 0; k < maxLanes; k++ {
		aos[k] = nativeopus.ExportTestNSQDelDecFillRandom(r)
	}
	coef := make([]int16, order)
	for i := range coef {
		coef[i] = int16(r.Int31())
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for k := 0; k < maxLanes; k++ {
			_ = nativeopus.ExportTestShortPredictionScalar(
				nativeopus.ExportTestNSQDelDecSLPCQ14(&aos[k]), base, coef, order)
		}
	}
}

// BenchmarkNSQShortPrediction_SoAPureGo — pure-Go SoA 4-lane kernel.
// Expected to be slower than 4× scalar because of the [4]opus_int32
// indexing overhead the Go compiler can't SIMD-ify.
func BenchmarkNSQShortPrediction_SoAPureGo(b *testing.B) {
	r := rand.New(rand.NewSource(1))
	const order = 16
	const base = 30
	const maxLanes = 4
	aos := make([]nativeopus.NSQ_del_dec_struct, maxLanes)
	for k := 0; k < maxLanes; k++ {
		aos[k] = nativeopus.ExportTestNSQDelDecFillRandom(r)
	}
	coef := make([]int16, order)
	for i := range coef {
		coef[i] = int16(r.Int31())
	}
	soa := nativeopus.ExportTestNSQDelDecAoStoSoA(aos, maxLanes)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = nativeopus.ExportTestShortPredictionSoA(soa, base, coef, order)
	}
}

// BenchmarkNSQShortPrediction_SoASIMD — arm64 NEON asm kernel. This
// is the target: one vld1q_s32 per tap, one SQDMULH, one ADD. If
// this doesn't beat 4× scalar by a comfortable margin, the whole
// SoA refactor is pointless.
func BenchmarkNSQShortPrediction_SoASIMD(b *testing.B) {
	if !nativeopus.ExportTestNSQSIMDAvailable() {
		b.Skip("NSQ SIMD not compiled in")
	}
	r := rand.New(rand.NewSource(1))
	const order = 16
	const base = 30
	const maxLanes = 4
	aos := make([]nativeopus.NSQ_del_dec_struct, maxLanes)
	for k := 0; k < maxLanes; k++ {
		aos[k] = nativeopus.ExportTestNSQDelDecFillRandom(r)
	}
	coef := make([]int16, order)
	for i := range coef {
		coef[i] = int16(r.Int31())
	}
	soa := nativeopus.ExportTestNSQDelDecAoStoSoA(aos, maxLanes)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = nativeopus.ExportTestShortPredictionSoASIMD(soa, base, coef, order)
	}
}

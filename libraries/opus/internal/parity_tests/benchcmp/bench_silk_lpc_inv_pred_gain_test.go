//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// Benchmarks for the three implementations of
// silk_LPC_inverse_pred_gain on a realistic order=16 Q12 vector. The
// kernel is called by silk_NLSF2A in a stabilisation retry loop (up
// to MAX_LPC_STABILIZE_ITERATIONS times per SILK frame), so even
// small per-call wins compound across encode sessions.
//
//   _Scalar   — pure Go scalar port (silk_LPC_inverse_pred_gain_c
//               with the SIMD dispatch disabled).
//   _SIMDRef  — pure Go 4-lane SIMD-style reference. Structurally
//               parallel, lets the compiler auto-vectorise bounded
//               arithmetic. Should be comparable to or faster than
//               scalar due to better ILP.
//   _Arch     — whatever the arch-aware dispatcher routes to
//               (SIMD path on arm64 default build, scalar otherwise).
//               This is what shipping code actually executes.

func benchLPCInvPredGainA(r *rand.Rand, order int) []int16 {
	A := make([]int16, order)
	for i := range A {
		A[i] = int16(r.Int31n(8000) - 4000)
	}
	return A
}

func BenchmarkSilkLPCInversePredGain_Scalar(b *testing.B) {
	r := rand.New(rand.NewSource(1))
	A := benchLPCInvPredGainA(r, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = nativeopus.ExportTestSilkLPCInversePredGain(A)
	}
}

func BenchmarkSilkLPCInversePredGain_SIMDRef(b *testing.B) {
	r := rand.New(rand.NewSource(1))
	A := benchLPCInvPredGainA(r, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = nativeopus.ExportTestSilkLPCInversePredGainSIMDRef(A)
	}
}

func BenchmarkSilkLPCInversePredGain_Arch(b *testing.B) {
	r := rand.New(rand.NewSource(1))
	A := benchLPCInvPredGainA(r, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = nativeopus.ExportTestSilkLPCInversePredGainArch(A)
	}
}

//go:build cgo && opus_strict

package benchcmp

import (
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_SilkLPCInversePredGain_SIMDRef asserts the pure-Go
// 4-lane SIMD-style reference produces bit-identical invGain_Q30 to
// the scalar C reference across a broad sweep of Q12 LPC coefficient
// vectors. This is the correctness gate for the NEON asm port:
// the asm must match this reference (which matches the scalar C).
//
// Coverage: orders {10, 12, 16, 24} × 50 seeded random trials, Q12
// values drawn from r.Int31n(8000)-4000 (realistic LPC range).
func TestParity_SilkLPCInversePredGain_SIMDRef(t *testing.T) {
	r := rand.New(rand.NewSource(2026))
	for _, order := range []int{10, 12, 16, 24} {
		for trial := 0; trial < 50; trial++ {
			A := make([]int16, order)
			for i := range A {
				A[i] = int16(r.Int31n(8000) - 4000)
			}
			wantC := cSilkLPCInversePredGain(A)
			gotRef := nativeopus.ExportTestSilkLPCInversePredGainSIMDRef(A)
			if wantC != gotRef {
				t.Fatalf("SIMD ref mismatch: order=%d trial=%d C=%d SIMDref=%d A=%v",
					order, trial, wantC, gotRef, A)
			}
			// Also cross-check against the scalar Go port, which is
			// what ships in the default build when the asm path is
			// off. If this diverges from cSilkLPCInversePredGain the
			// bug is in the shared scalar path, not the SIMD ref.
			gotGoScalar := nativeopus.ExportTestSilkLPCInversePredGain(A)
			if wantC != gotGoScalar {
				t.Fatalf("scalar Go mismatch: order=%d trial=%d C=%d Go=%d A=%v",
					order, trial, wantC, gotGoScalar, A)
			}
		}
	}
}

// TestParity_SilkLPCInversePredGain_Arch walks the arch-aware
// dispatcher when the SIMD path is compiled in. The dispatcher is
// still required to be bit-exact with the scalar C (per libopus's
// OPUS_CHECK_ASM assertion in LPC_inv_pred_gain_neon_intr.c line
// 283-284), so any drift here is a silent SILK-encoder-breaking bug.
func TestParity_SilkLPCInversePredGain_Arch(t *testing.T) {
	if !nativeopus.ExportTestSilkLPCInversePredGainSIMDAvailable() {
		t.Skip("silk_LPC_inverse_pred_gain SIMD not compiled in (opus_nosimd or opus_strict)")
	}
	r := rand.New(rand.NewSource(2027))
	for _, order := range []int{10, 12, 16, 24} {
		for trial := 0; trial < 50; trial++ {
			A := make([]int16, order)
			for i := range A {
				A[i] = int16(r.Int31n(8000) - 4000)
			}
			wantC := cSilkLPCInversePredGain(A)
			gotArch := nativeopus.ExportTestSilkLPCInversePredGainArch(A)
			if wantC != gotArch {
				t.Fatalf("arch-dispatched SIMD mismatch: order=%d trial=%d C=%d Arch=%d A=%v",
					order, trial, wantC, gotArch, A)
			}
		}
	}
}

// TestParity_SilkLPCInversePredGainQASIMDRef_InPlace confirms the QA
// entry point (direct call into the inner kernel) is bit-exact with
// the scalar C kernel after both mutate their A_QA buffer. This is
// the invariant that the asm port must also honour — the scalar C
// kernel modifies A_QA during the unroll, and consumers depend on
// that side-effect in the NLSF2A stabilisation loop.
func TestParity_SilkLPCInversePredGainQASIMDRef_InPlace(t *testing.T) {
	r := rand.New(rand.NewSource(2028))
	for _, order := range []int{10, 12, 16, 24} {
		for trial := 0; trial < 50; trial++ {
			// Build an Atmp_QA as the wrapper does (Q12<<12 = Q24).
			AQA := make([]int32, order)
			for i := range AQA {
				v := int32(r.Int31n(8000)-4000) << 12
				AQA[i] = v
			}
			refBuf := append([]int32(nil), AQA...)
			simdBuf := append([]int32(nil), AQA...)

			want := cSilkLPCInversePredGainQA(refBuf, order)
			got := nativeopus.ExportTestSilkLPCInversePredGainQASIMDRef(simdBuf, order)
			if want != got {
				t.Fatalf("QA return mismatch: order=%d trial=%d C=%d SIMDref=%d",
					order, trial, want, got)
			}
			// When the gain is non-zero, the kernel guarantees A_QA is
			// mutated; when it's zero we bail early and A_QA state is
			// unspecified. Only check mutation parity on stable cases.
			if want != 0 {
				for i := range refBuf {
					if refBuf[i] != simdBuf[i] {
						t.Fatalf("QA A_QA mutation mismatch at [%d]: order=%d trial=%d C=%d SIMDref=%d",
							i, order, trial, refBuf[i], simdBuf[i])
					}
				}
			}
		}
	}
}

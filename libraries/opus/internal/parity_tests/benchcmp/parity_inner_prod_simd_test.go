//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// TestParity_CeltInnerProd_SIMDvsScalar cross-checks the arch-aware
// dispatch (SIMD when compiled in) against the scalar Go path. They
// won't be bit-exact — the SIMD kernel accumulates 4-lane partial sums
// and tree-reduces, which differs in rounding order from the scalar
// left-to-right sum — so we allow a small relative tolerance that
// grows with N.
func TestParity_CeltInnerProd_SIMDvsScalar(t *testing.T) {
	if !nativeopus.ExportTestInnerProdSIMDAvailable() {
		t.Skip("inner_prod SIMD not compiled in (opus_strict or opus_nosimd)")
	}
	r := rand.New(rand.NewSource(42))
	for _, N := range []int{1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 17, 31, 32, 100, 480} {
		for run := 0; run < 5; run++ {
			x := randSig(r, N, 0.5)
			y := randSig(r, N, 0.5)
			scalar := nativeopus.ExportTestCeltInnerProdC(x, y, N)
			simd := nativeopus.ExportTestCeltInnerProdArch(x, y, N)
			// Tolerance: ~1 ULP per accumulated product. Use a relative
			// check with a small absolute floor.
			rel := float32(math.Abs(float64(scalar-simd))) /
				float32(math.Max(math.Abs(float64(scalar)), 1e-12))
			if rel > 1e-5 {
				t.Errorf("N=%d run=%d: scalar=%g simd=%g rel=%g", N, run, scalar, simd, rel)
			}
		}
	}
}

func TestParity_DualInnerProd_SIMDvsScalar(t *testing.T) {
	if !nativeopus.ExportTestInnerProdSIMDAvailable() {
		t.Skip("inner_prod SIMD not compiled in")
	}
	r := rand.New(rand.NewSource(43))
	for _, N := range []int{1, 4, 8, 20, 160, 480} {
		for run := 0; run < 5; run++ {
			x := randSig(r, N, 0.5)
			y01 := randSig(r, N, 0.5)
			y02 := randSig(r, N, 0.5)
			s1, s2 := nativeopus.ExportTestDualInnerProdC(x, y01, y02, N)
			g1, g2 := nativeopus.ExportTestDualInnerProdArch(x, y01, y02, N)
			rel1 := float32(math.Abs(float64(s1-g1))) /
				float32(math.Max(math.Abs(float64(s1)), 1e-12))
			rel2 := float32(math.Abs(float64(s2-g2))) /
				float32(math.Max(math.Abs(float64(s2)), 1e-12))
			if rel1 > 1e-5 || rel2 > 1e-5 {
				t.Errorf("N=%d run=%d: scalar=(%g,%g) simd=(%g,%g) rel=(%g,%g)",
					N, run, s1, s2, g1, g2, rel1, rel2)
			}
		}
	}
}

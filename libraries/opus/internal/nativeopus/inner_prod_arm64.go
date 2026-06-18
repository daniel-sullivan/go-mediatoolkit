//go:build arm64 && !opus_nosimd && !opus_strict

package nativeopus

// celtInnerProdSIMD — arm64 NEON port of celt_inner_prod_neon
// (libopus/celt/arm/pitch_neon_intr.c float path). See the .s file for
// the encoding notes. FMLA-fused; NOT strict-parity.
//
//go:noescape
func celtInnerProdSIMD(x, y *float32, N int) float32

// dualInnerProdSIMD — arm64 NEON port of dual_inner_prod_neon.
//
//go:noescape
func dualInnerProdSIMD(x, y01, y02 *float32, N int, xy1, xy2 *float32)

const innerProdSIMDAvailable = true

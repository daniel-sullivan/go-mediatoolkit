//go:build !((arm64 || amd64) && !opus_nosimd && !opus_strict)

package nativeopus

// Fallback when the inner-product SIMD path is compiled out:
//   - non-arm64/amd64 platforms (no asm kernel)
//   - -tags=opus_nosimd
//   - -tags=opus_strict (4-lane reduction isn't bit-exact with scalar
//     left-to-right summation)

func celtInnerProdSIMD(x, y *float32, N int) float32 { _ = x; _ = y; _ = N; return 0 }
func dualInnerProdSIMD(x, y01, y02 *float32, N int, xy1, xy2 *float32) {
	_, _, _, _, _, _ = x, y01, y02, N, xy1, xy2
}

const innerProdSIMDAvailable = false

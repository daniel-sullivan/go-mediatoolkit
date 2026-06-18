//go:build amd64 && !opus_nosimd && !opus_strict

package nativeopus

// celtInnerProdSIMD — amd64 SSE port of celt_inner_prod_sse
// (libopus/celt/x86/pitch_sse.c). See the .s file for notes.
// Baseline SSE2; no FMA. NOT strict-parity (tree reduction differs).
//
//go:noescape
func celtInnerProdSIMD(x, y *float32, N int) float32

// dualInnerProdSIMD — amd64 SSE port of dual_inner_prod_sse.
//
//go:noescape
func dualInnerProdSIMD(x, y01, y02 *float32, N int, xy1, xy2 *float32)

const innerProdSIMDAvailable = true

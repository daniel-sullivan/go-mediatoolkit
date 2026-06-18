//go:build mp3_strict

package nativemp3

// StrictMode is true in the parity build (the mp3_strict build tag). Code
// with a floating-point fast path — compile-time SIMD or FMA fusing — can
// branch on this constant, but prefer file-level build tags (the
// *_fp_strict.go / *_fp_default.go split) for code that compiles to
// entirely different instructions. The integer slices (main-bits,
// Huffman tree traversal) are bit-identical regardless of this flag.
//
// FP-parity convention — accepted pow/log10 residual. Under mp3_strict the
// pinned floating-point set (calc_xmin / calc_noise / calc_noise_core_c, the
// quantizer, IMDCT, synthesis) is bit-exact against the cgo oracle built with
// -ffp-contract=off. The ONE intentional exception is the ATH-shaping helpers
// (athAdjust / athmdct / computeATH in quantize_pvt.go): they rest on
// pow()/log10(), and Go's math.Pow/math.Log10 are not bit-identical to the
// platform libm the oracle links (whereas math.Cos/Sin/Exp/Log are). Their ATH
// energy floors land within <=2 ULP of the oracle, NOT byte-for-byte. This is
// an accepted, environmental libm-vs-Go-stdlib gap — the same convention opus's
// silk_FLP and the flac port follow (no bit-pinning of pow/log10). Everything
// outside those ATH inputs stays bit-exact.
const StrictMode = true

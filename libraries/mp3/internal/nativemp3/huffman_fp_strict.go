//go:build mp3_strict

package nativemp3

// Strict-mode float32 helpers for minimp3's L3_huffman dequantization.
//
// In the parity oracle minimp3 is compiled with -ffp-contract=off, so every
// `a + b*c` in L3_pow_43's polynomial evaluation is two separately rounded
// float operations: a float32 multiply producing a rounded product, then a
// float32 add. Go's backend auto-fuses `a + b*c` into an FMA on arm64,
// which would diverge in the last ULP. Routing each multiply through a
// //go:noinline helper makes the product an opaque function-call return that
// Go's SSA cannot pattern-match back into a fused multiply-add. The other
// helpers are likewise //go:noinline so each individual `+` / `-` / `/` is a
// single round-to-nearest-even float32 operation, matching clang under
// -ffp-contract=off -fno-vectorize. (Same technique as the opus and flac
// ports.)

//go:noinline
func f32mul(a, b float32) float32 { return a * b }

//go:noinline
func f32add(a, b float32) float32 { return a + b }

//go:noinline
func f32sub(a, b float32) float32 { return a - b }

//go:noinline
func f32div(a, b float32) float32 { return a / b }

//go:build opus_strict

package nativeopus

// Float32 multiply-add helpers — bit-exact parity (strict) variant.
//
// Selected via `-tags=opus_strict`. The companion file fma_default.go
// provides the FMA-fused variant that ships as the default build. Use
// `opus_strict` when you need bit-exact output against the `-ffp-
// contract=off` reference C build (parity testing, regression bisect).
// For production use, prefer the default build — the FMA variant is
// ~2× faster on encode paths and is perceptually identical (PSNR
// 117 dB+ vs this path, well below audibility).
//
// Goal: produce bit-exact output matching the Cgo oracle, which is
// compiled with `-ffp-contract=off` (see mise.toml's CGO_CFLAGS). On
// both sides each `a + b*c` is evaluated as two separately rounded
// operations — first a float32 multiply producing a rounded product,
// then a float32 add. No fused multiply-add, no single-rounded
// infinite-precision product.
//
// Why not FMA? Clang at -O2 WITH FMA fusion also applies instruction-
// level reassociation (InstCombine / Reassociate passes) on chained
// float arithmetic, which reorders adds inside multi-step
// accumulations. Disabling those passes from cgo is impractical
// (requires -mllvm flags that leak optimisation decisions outside our
// control). Forcing non-FMA on both sides skips the whole
// reassociation-under-FMA-licensing question: each operation is a
// straight IEEE 754 mul or add with no compiler freedom to reorder.
//
// Defeating Go's SSA fusion: Go's arm64 backend auto-fuses `a + b*c`
// into FMADDS and will NOT be deterred by unsafe-pointer round-trips,
// math.Float32bits round-trips, or package-var stores — every one of
// those we tested still produced FMADDS. The only reliable technique
// is to route the multiply through a //go:noinline helper so the
// product is a real function-call return value that Go's SSA can't
// see past.
//
// Wrapper inlining: the outer `fma_*` wrappers below are *not*
// //go:noinline — their bodies just chain two already-noinline calls
// (`add_f32(a, mul_f32(b, c))`), so inlining the wrapper just folds
// those two calls into the caller. The mul_f32 call still produces an
// opaque return value that SSA cannot pattern-match back into
// `Add(x, Mul(y, z))`, so no FMA fusion is possible. Net: 2 BL per
// fma_add call instead of 3, at 700+ call sites across the port.

//go:noinline
func mul_f32(a, b float32) float32 { return a * b }

// add_f32 / sub_f32 — the //go:noinline is not just defensive: on
// Go 1.26 arm64 an *inline* float32 `a+b` is sometimes compiled in
// a way that produces a different last-bit rounding than what the
// arm64 FADD.S instruction alone would emit, whereas the function-
// call form reliably produces a single-rounded float32 add matching
// clang under -ffp-contract=off. We saw this concretely on inputs
// like 0xbe0b771c + 0x3b81d750, where the inline form produced a
// mantissa ending in an odd ULP while the correct round-to-nearest-
// even result is the adjacent even mantissa (which C and the
// //go:noinline helper both produce). Routing every float32 `+`
// and `-` through add_f32 / sub_f32 in the SILK _FLP ports keeps
// parity with the Cgo oracle regardless of the root cause.

//go:noinline
func add_f32(a, b float32) float32 { return a + b }

//go:noinline
func sub_f32(a, b float32) float32 { return a - b }

//go:noinline
func neg_mul_f32(a, b float32) float32 { return -(a * b) }

// fma_add returns a + b*c with a separate-round multiply then add.
// Matches C `-ffp-contract=off` behavior bit-for-bit. Not //go:noinline —
// see wrapper-inlining note in the package comment above.
func fma_add(a, b, c float32) float32 { return add_f32(a, mul_f32(b, c)) }

// fma_sub returns a - b*c, similarly non-fused.
func fma_sub(a, b, c float32) float32 { return sub_f32(a, mul_f32(b, c)) }

// fma_rsub returns b*c - a, non-fused.
func fma_rsub(a, b, c float32) float32 { return sub_f32(mul_f32(b, c), a) }

// fneg_mul returns -(a*b). There is no add, so no FMA concern; a
// single FNMULS is emitted. Kept for naming symmetry with the FMA
// helpers and for ports of `-a*b`-pattern C expressions.
func fneg_mul(a, b float32) float32 { return neg_mul_f32(a, b) }

// float64 variants — SILK _FLP inner products accumulate in double
// and the C oracle is compiled with -ffp-contract=off, so we need
// separately-rounded float64 multiplies and adds here too. Go's
// arm64 backend fuses float64 a+b*c into FMADDD just like it does
// FMADDS for float32.

//go:noinline
func mul_f64(a, b float64) float64 { return a * b }

//go:noinline
func add_f64(a, b float64) float64 { return a + b }

//go:noinline
func sub_f64(a, b float64) float64 { return a - b }

// fma_add64 returns a + b*c with a separate-round multiply then add,
// using float64. Not //go:noinline — see wrapper-inlining note above.
func fma_add64(a, b, c float64) float64 { return add_f64(a, mul_f64(b, c)) }

// fma_sub64 returns a - b*c, non-fused float64.
func fma_sub64(a, b, c float64) float64 { return sub_f64(a, mul_f64(b, c)) }

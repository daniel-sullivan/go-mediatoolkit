//go:build flac_strict

package nativeflac

// Strict-mode float64 helpers for the LPC Levinson-Durbin recursion
// (lpc.c FLAC__lpc_compute_lp_coefficients) parity.
//
// The cgo parity oracle compiles lpc.c with `-ffp-contract=off`
// (see the parity packages' #cgo CFLAGS), so clang does NOT fuse any
// `a*b+c` into an fmadd — every multiply and every add is a separately
// rounded double operation. Go's arm64 backend, by contrast, will fuse
// `a + b*c` into FMADD and `a - b*c` into FNMSUB unless prevented.
//
// Routing each multiply / add / subtract through a //go:noinline helper
// makes the intermediate an opaque function-call return value that Go's
// SSA cannot pattern-match back into a fused multiply-add, so each step
// rounds independently and matches the un-contracted clang oracle
// bit-for-bit. (Same technique as the opus port's fma_strict.go and the
// f32 family in window_fp_strict.go.)
//
// f64fma here is therefore the SEPARATELY-ROUNDED form: a*b rounded,
// then +c rounded — NOT math.FMA. The default build (lpc_fp_default.go)
// uses the genuinely fused math.FMA, which is not a parity target.

//go:noinline
func f64mul(a, b float64) float64 { return a * b }

//go:noinline
func f64add(a, b float64) float64 { return a + b }

//go:noinline
func f64sub(a, b float64) float64 { return a - b }

// f64fma computes a*b+c with the product and the sum each rounded
// separately (no fusion), mirroring clang under -ffp-contract=off.
func f64fma(a, b, c float64) float64 { return f64add(f64mul(a, b), c) }

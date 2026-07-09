//go:build aec_strict

package aec3

// Strict floating-point composites for bit-exact parity against the
// C++ oracle, which is compiled with -ffp-contract=off (see
// aec/oracle/fetch.sh). The Go spec allows the compiler to fuse
// `a + b*c` into a single-rounding FMA (and the arm64 backend does),
// which diverges from the oracle's separately-rounded multiply and
// add by 1 ulp. Routing the multiply and the add/sub through
// //go:noinline primitives forces a rounding boundary between them,
// exactly like the opus port's fma_strict.go and the flac port's f64
// helpers. The composite wrappers themselves may inline — the
// primitives inside them still can't fuse across their call boundary.

//go:noinline
func mul32(a, b float32) float32 { return a * b }

//go:noinline
func add32(a, b float32) float32 { return a + b }

//go:noinline
func sub32(a, b float32) float32 { return a - b }

// mla returns a + b*c, separately rounded.
func mla(a, b, c float32) float32 { return add32(a, mul32(b, c)) }

// muladd returns a*b + c*d, separately rounded.
func muladd(a, b, c, d float32) float32 { return add32(mul32(a, b), mul32(c, d)) }

// mulsub returns a*b - c*d, separately rounded.
func mulsub(a, b, c, d float32) float32 { return sub32(mul32(a, b), mul32(c, d)) }

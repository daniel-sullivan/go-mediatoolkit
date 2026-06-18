//go:build arm64 && !opus_nosimd

package nativeopus

// xcorrKernelSIMD implements the xcorr_kernel_c MAC loop via arm64
// NEON assembly. See xcorr_kernel_arm64.s for the encoding notes.
// Parity is bit-exact with the scalar path: each lane does a
// separately-rounded FMUL.4S + FADD.4S.
//
//go:noescape
func xcorrKernelSIMD(x, y *float32, sum *[4]float32, ln int)

const xcorrSIMDAvailable = true

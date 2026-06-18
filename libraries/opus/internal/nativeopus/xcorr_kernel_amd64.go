//go:build amd64 && !opus_nosimd

package nativeopus

// xcorrKernelSIMD implements the xcorr_kernel_c MAC loop via amd64
// SSE assembly. See xcorr_kernel_amd64.s. Bit-exact with the scalar
// path (separately-rounded MULPS + ADDPS per lane).
//
//go:noescape
func xcorrKernelSIMD(x, y *float32, sum *[4]float32, ln int)

const xcorrSIMDAvailable = true

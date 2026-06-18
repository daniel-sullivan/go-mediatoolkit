//go:build (!arm64 && !amd64) || opus_nosimd

package nativeopus

// xcorrKernelSIMD is unreachable on non-arm64 builds (xcorrSIMDAvailable
// is false so the dispatcher in pitch.go never calls it). The symbol
// exists purely so the package compiles on all architectures.
func xcorrKernelSIMD(x, y *float32, sum *[4]float32, ln int) {
	panic("xcorrKernelSIMD called on a build without a SIMD implementation")
}

const xcorrSIMDAvailable = false

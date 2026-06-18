//go:build !arm64

package nativeflac

// These symbols are unreachable on non-arm64 builds: fixedAbsSIMDAvailable
// and partitionAbsSumSIMDAvailable are false, so the dispatchers in
// fixed_encode.go and encoder_subframe.go never call them. They exist
// only so the package compiles on every architecture.

func fixedAbsErrors4NEON(q0, q1, q2, q3 *int32, prevErr *[16]int32, iters int, totals *[5]uint32) {
	panic("fixedAbsErrors4NEON called on a build without the NEON kernel")
}

const fixedAbsSIMDAvailable = false

func partitionAbsSum32NEON(p *int32, vecs int) uint32 {
	panic("partitionAbsSum32NEON called on a build without the NEON kernel")
}

const partitionAbsSumSIMDAvailable = false

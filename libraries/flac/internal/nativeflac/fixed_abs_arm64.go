//go:build arm64

package nativeflac

// fixedAbsErrors4NEON runs the quarter-split NEON vector loop of the
// fixed-predictor abs-error accumulation (see fixed_abs_arm64.s). It is
// integer-exact (int32 two's-complement) and so compiled into BOTH the
// default and flac_strict builds; the strict parity gate verifies
// bit-exactness. The Go caller seeds prevErr, supplies the four quarter
// pointers, and handles the data_len%4 remainder.
//
//go:noescape
func fixedAbsErrors4NEON(q0, q1, q2, q3 *int32, prevErr *[16]int32, iters int, totals *[5]uint32)

// fixedAbsSIMDAvailable reports that the NEON fixed-predictor kernel is
// present.
const fixedAbsSIMDAvailable = true

// partitionAbsSumSIMDAvailable reports that the NEON partition abs-sum
// kernel (see partition_sums_arm64.s) is present.
const partitionAbsSumSIMDAvailable = true

// partitionAbsSum32NEON sums abs(int32) over vecs*4 samples into a single
// uint32 (wrapping) accumulator and returns the 4-lane-reduced total; see
// partition_sums_arm64.s. The Go caller handles the <4 remainder.
//
//go:noescape
func partitionAbsSum32NEON(p *int32, vecs int) uint32

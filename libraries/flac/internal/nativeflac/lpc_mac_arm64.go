//go:build arm64

package nativeflac

// lpcResidualMACNEON computes the LPC encoder residual for groups of four
// output samples using the arm64 NEON kernel in lpc_mac_arm64.s. It is
// integer-exact (int32 two's-complement, identical to the scalar path)
// and therefore compiled into BOTH the default and flac_strict builds;
// the strict parity gate verifies bit-exactness. Returns the number of
// output samples consumed (a multiple of 4); the caller computes the
// remaining tail with the scalar unrolled path.
//
//go:noescape
func lpcResidualMACNEON(data *int32, dataLen int, qlpCoeff *int32, order int, lpQuantization int, residual *int32) int

// lpcMACAvailable reports that the NEON residual kernel is present.
const lpcMACAvailable = true

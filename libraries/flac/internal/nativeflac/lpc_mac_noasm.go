//go:build !arm64

package nativeflac

// lpcResidualMACNEON is unreachable on non-arm64 builds: lpcMACAvailable
// is false, so the dispatcher in lpc_encode.go never calls it. The symbol
// exists only so the package compiles on every architecture.
func lpcResidualMACNEON(data *int32, dataLen int, qlpCoeff *int32, order int, lpQuantization int, residual *int32) int {
	panic("lpcResidualMACNEON called on a build without the NEON kernel")
}

// lpcMACAvailable reports that no NEON residual kernel is present.
const lpcMACAvailable = false

//go:build flac_strict || !arm64

package nativeflac

// Strict / portable autocorrelation body.
//
// This is the exact scalar port of
// deduplication/lpc_compute_autocorrelation_intrin.c, routing every
// multiply/add through the f64 //go:noinline helpers (flac_strict) so
// Go's arm64 backend cannot contract `a*b+c` into an FMADDD. Under
// flac_strict it matches the cgo oracle (compiled -ffp-contract=off)
// bit-for-bit; the strict parity gate asserts that. On non-arm64 it is
// the only available implementation (no NEON kernel), and the default
// f64 helpers there permit ordinary fusion — that build is not a parity
// target.
func lpcAutocorrelationMaxLag(data []float32, dataLen uint32, maxLag uint32, autoc []float64) {
	for i := uint32(0); i < maxLag; i++ {
		autoc[i] = 0.0
	}
	for i := uint32(0); i < maxLag; i++ {
		for j := uint32(0); j <= i; j++ {
			autoc[j] = f64add(autoc[j], f64mul(float64(data[i]), float64(data[i-j])))
		}
	}
	for i := maxLag; i < dataLen; i++ {
		for j := uint32(0); j < maxLag; j++ {
			autoc[j] = f64add(autoc[j], f64mul(float64(data[i]), float64(data[i-j])))
		}
	}
}

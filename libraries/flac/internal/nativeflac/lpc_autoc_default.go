//go:build arm64 && !flac_strict

package nativeflac

// lpcAutocorrelationNEON computes the first 16 autocorrelation lags
// (autoc[0..15]) of the float32 windowed signal with a float64x2 NEON
// kernel using 8 independent accumulators (lpc_autoc_arm64.s). It
// reassociates the float64 reduction relative to the scalar
// -ffp-contract=off oracle, so it is FP-NON-bit-exact and lives in the
// default build only; the flac_strict build uses the scalar body in
// lpc_autoc_strict.go. data must have length >= dataLen and autoc must
// have room for 16 entries.
//
//go:noescape
func lpcAutocorrelationNEON(data *float32, dataLen int, autoc *float64)

// lpcAutocorrelationMaxLag — default-build float64x2 NEON autocorrelation
// body. The NEON kernel always populates 16 lags into a local buffer with
// multiple in-flight FMADDD chains; we copy back the meaningful prefix the
// caller's bucket (maxLag ∈ {8,12,16}) actually uses. Mirrors
// lpc_compute_autocorrelation_intrin_neon.c.
func lpcAutocorrelationMaxLag(data []float32, dataLen uint32, maxLag uint32, autoc []float64) {
	var buf [16]float64
	var dptr *float32
	if dataLen > 0 {
		dptr = &data[0]
	}
	lpcAutocorrelationNEON(dptr, int(dataLen), &buf[0])
	for i := uint32(0); i < maxLag; i++ {
		autoc[i] = buf[i]
	}
}

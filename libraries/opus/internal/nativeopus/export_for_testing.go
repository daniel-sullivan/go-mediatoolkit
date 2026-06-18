package nativeopus

// Exported accessors used by parity tests in
// libraries/opus/internal/parity_tests/benchcmp/ to call into
// unexported (lowercase, C-name-preserving) functions
// from outside the package. Do not depend on these from production
// code — they exist solely to keep the port's snake_case identifiers
// while still allowing external bit-exactness checks against the C
// oracle.

func ExportTestCeltLog2(x float32) float32          { return celt_log2(x) }
func ExportTestCeltExp2(x float32) float32          { return celt_exp2(x) }
func ExportTestCeltCosNorm2(x float32) float32      { return celt_cos_norm2(x) }
func ExportTestCeltAtanNorm(x float32) float32      { return celt_atan_norm(x) }
func ExportTestCeltAtan2pNorm(y, x float32) float32 { return celt_atan2p_norm(y, x) }
func ExportTestFastAtan2f(y, x float32) float32     { return fast_atan2f(y, x) }
func ExportTestFloat2Int16(x float32) int16         { return FLOAT2INT16(x) }
func ExportTestFloat2Int(x float32) int32           { return float2int(x) }
func ExportTestIsqrt32(v uint32) uint               { return isqrt32(v) }

func ExportTestCeltFloat2Int16C(in []float32, out []int16, n int) {
	celt_float2int16_c(in, out, n)
}
func ExportTestOpusLimit2CheckWithin1C(samples []float32, n int) int {
	return opus_limit2_checkwithin1_c(samples, n)
}

// ExportTestCeltFloat2Int16Scalar runs the pure-Go scalar path
// (math.RoundToEven) regardless of build tags, for SIMD-vs-scalar
// parity tests.
func ExportTestCeltFloat2Int16Scalar(in []float32, out []int16, n int) {
	for i := 0; i < n; i++ {
		out[i] = FLOAT2INT16(in[i])
	}
}

// ExportTestCeltFloat2Int16SIMD calls the NEON kernel directly
// (panics if it's a no-op stub — guard with the availability flag).
func ExportTestCeltFloat2Int16SIMD(in []float32, out []int16, n int) {
	if n == 0 {
		return
	}
	celtFloat2Int16SIMD(&in[0], &out[0], n)
}

// ExportTestFloat2Int16SIMDAvailable — true when the NEON float2int16
// path is compiled in.
func ExportTestFloat2Int16SIMDAvailable() bool { return float2int16SIMDAvailable }

// ExportTestOpusLimit2CheckWithin1Scalar runs the scalar-only path
// (always returns 0 as "unknown") regardless of build tags.
func ExportTestOpusLimit2CheckWithin1Scalar(samples []float32, n int) int {
	if n <= 0 {
		return 1
	}
	for i := 0; i < n; i++ {
		v := samples[i]
		v = float32(FMAX(v, -2.0))
		v = float32(FMIN(v, 2.0))
		samples[i] = v
	}
	return 0
}

// ExportTestOpusLimit2CheckWithin1SIMD calls the NEON kernel directly.
func ExportTestOpusLimit2CheckWithin1SIMD(samples []float32, n int) int32 {
	if n == 0 {
		return 1
	}
	return opusLimit2CheckWithin1SIMD(&samples[0], n)
}

// ExportTestLimit2SIMDAvailable — true when the NEON limit2 path is
// compiled in.
func ExportTestLimit2SIMDAvailable() bool { return limit2SIMDAvailable }

package nativemp3

// Exported test hooks for the stereo-decoding parity oracle.
//
// The Layer III stereo functions (L3_midside_stereo,
// L3_intensity_stereo_band, L3_stereo_top_band, L3_stereo_process,
// L3_intensity_stereo) are 1:1 translations of minimp3's `static` helpers and
// have no place in the public surface, so they stay unexported. The cgo
// parity package internal/parity_tests/stereo-decoding cannot live inside
// nativemp3 (it compiles the minimp3 oracle), so it reaches the port through
// the thin pass-throughs below. Each wrapper is a verbatim call to the
// unexported function it shadows; they exist solely so the parity suite can
// assert the Go port matches the vendored minimp3 bit-for-bit.
// (L3_ldexp_q2 is exported directly as L3Ldexp by the dequantize slice.)

// L3MidsideStereo exposes l3MidsideStereo for the parity oracle.
func L3MidsideStereo(left []float32, base, n int) { l3MidsideStereo(left, base, n) }

// L3IntensityStereoBand exposes l3IntensityStereoBand for the parity oracle.
func L3IntensityStereoBand(left []float32, base, n int, kl, kr float32) {
	l3IntensityStereoBand(left, base, n, kl, kr)
}

// L3StereoTopBand exposes l3StereoTopBand for the parity oracle.
func L3StereoTopBand(right []float32, rightBase int, sfb []byte, nbands int, maxBand *[3]int) {
	l3StereoTopBand(right, rightBase, sfb, nbands, maxBand)
}

// L3StereoProcess exposes l3StereoProcess for the parity oracle.
func L3StereoProcess(left []float32, istPos, sfb, hdr []byte, maxBand *[3]int, mpeg2Sh int) {
	l3StereoProcess(left, istPos, sfb, hdr, maxBand, mpeg2Sh)
}

// L3IntensityStereo exposes l3IntensityStereo for the parity oracle.
func L3IntensityStereo(left []float32, istPos []byte, gr []L3GrInfo, hdr []byte) {
	l3IntensityStereo(left, istPos, gr, hdr)
}

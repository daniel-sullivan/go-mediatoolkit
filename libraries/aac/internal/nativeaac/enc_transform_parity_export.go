// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Thin exported wrappers around the forward (analysis) MDCT kernels
// (enc_transform.go) so the cgo parity oracle in
// internal/parity_tests/enc-analysis-filterbank can drive them without being
// in-package. They add no logic — they forward 1:1, converting the oracle's
// flat int16 (re,im) FIXP_WTP window slopes into in-package fixSTP slices and
// carrying the persistent mdct_t state across consecutive forward blocks (the
// analysis MDCT is stateful: each block's left window slope is the previous
// block's right slope). The production encode path uses the unexported forms
// with the ported window ROM.

// NewEncMdctState allocates an MdctState with a zeroed overlap buffer and runs
// mdct_init, mirroring the encoder's per-channel init. The forward MDCT shares
// the same mdct_t persistent type as the decoder (prev_fr / prev_wrs / prev_tl).
func NewEncMdctState(overlapBufferSize int) *MdctState {
	s := new(MdctState)
	mdctInit(&s.h, make([]int32, overlapBufferSize), overlapBufferSize)
	return s
}

// MdctBlockFwd runs the ported forward mdct_block over timeData (INT_PCM int16,
// length noInSamples) writing nSpec spectra of tl FIXP_DBL lines into mdctData,
// and returns nSpec*tl. wrs is the genuine FDKgetWindowSlope right slope (flat
// int16 [re,im]); fr its length. The per-spectrum block exponents are written
// into mdctDataE (length nSpec).
func (s *MdctState) MdctBlockFwd(timeData []int16, noInSamples int, mdctData []int32,
	nSpec, tl int, wrs []int16, fr int, mdctDataE []int16) int {
	return mdctBlockFwd(&s.h, timeData, noInSamples, mdctData, nSpec, tl,
		packFixSTP(wrs), fr, mdctDataE)
}

// WindowSlopeRadix2Flat returns the ported radix-2 window slope
// (getWindowSlopeRadix2) for the given length/shape as a flat int16
// [re0,im0,re1,im1,...] of length/2 pairs, so the oracle can verify it equals
// the genuine FDKgetWindowSlope ROM the encoder analysis MDCT selects.
func WindowSlopeRadix2Flat(length, shape int) []int16 {
	w := getWindowSlopeRadix2(length, uint8(shape))
	out := make([]int16, 0, length)
	for i := 0; i < length/2; i++ {
		out = append(out, w[i].re, w[i].im)
	}
	return out
}

// TransformReal runs the ported FDKaacEnc_Transform_Real over pTimeData (int16,
// length frameLength) into mdctData, selecting numSpec/fr from blockType and
// fetching the right window slope via the shared radix-2 selector (same ROM the
// oracle's FDKgetWindowSlope returns). Returns (rc, mdctDataE, prevWindowShape).
func (s *MdctState) TransformReal(pTimeData []int16, mdctData []int32,
	blockType, windowShape, prevWindowShape, frameLength int) (rc, mdctDataE, newPrevWindowShape int) {
	pws := prevWindowShape
	var e int
	rc = transformReal(pTimeData, mdctData, blockType, windowShape, &pws, &s.h, frameLength, &e)
	return rc, e, pws
}

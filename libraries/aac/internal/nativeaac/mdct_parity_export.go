// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Thin exported wrappers around the unexported fixed-point inverse-MDCT kernels
// (mdct.go) so the cgo parity oracle in internal/parity_tests/filterbank can
// drive them without being in-package. These add no logic — they forward 1:1,
// converting the oracle's flat int16 (re,im) FIXP_WTP/FIXP_STP ROM into the
// in-package fixSTP slices and exposing the persistent mdct_t overlap-add state
// across a sequence of imlt_block calls. The production decode path uses the
// unexported forms with the ported ROM.

// MdctState is an opaque handle over a ported mdct_t the parity oracle can carry
// across consecutive ImltBlock calls (the IMDCT is stateful: each block folds
// against, then overwrites, the 50% overlap carry).
type MdctState struct {
	h mdctT
}

// NewMdctState allocates an MdctState with a zeroed overlap buffer of
// overlapBufferSize int32 words and runs mdct_init over it, mirroring the C
// FDKmemclear(overlap)+mdct_init the decoder does once per channel.
func NewMdctState(overlapBufferSize int) *MdctState {
	s := new(MdctState)
	mdctInit(&s.h, make([]int32, overlapBufferSize), overlapBufferSize)
	return s
}

// ImltBlock runs the ported imlt_block over spectrum (length nSpec*tl, modified
// in place by the inner DCT/de-scale) writing time samples into output, and
// returns the number of output samples. twiddle/sinTwiddle/sinStep are the
// genuine dct_getTables ROM for tl; wls/wrs the genuine window slopes (all flat
// int16 [re,im]); scalefactor the per-spectrum input exponents. flags carries
// MLT_FLAG_CURR_ALIAS_SYMMETRY (0 on AAC-LC).
func (s *MdctState) ImltBlock(output, spectrum []int32, scalefactor []int16,
	nSpec, noOutSamples, tl int, wls []int16, fl int, wrs []int16, fr int,
	gain int32, flags, sinStep int, twiddle, sinTwiddle []int16) int {
	return imltBlock(&s.h, output, spectrum, scalefactor, nSpec, noOutSamples,
		tl, packFixSTP(wls), fl, packFixSTP(wrs), fr, gain, flags, sinStep,
		packFixSTP(twiddle), packFixSTP(sinTwiddle))
}

// ScaleValuesSaturateDst exposes scaleValuesSaturateDst so the oracle can match
// the AAC-LC FrequencyToTime tail (block.cpp:1240) that scales the imlt_block
// output by MDCT_OUT_HEADROOM - aacOutDataHeadroom into the PCM buffer.
func ScaleValuesSaturateDst(dst, src []int32, length int, scalefactor int32) {
	scaleValuesSaturateDst(dst, src, length, scalefactor)
}

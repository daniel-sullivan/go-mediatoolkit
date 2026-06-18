// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Joint-stereo decode tools — the ms-stereo / intensity-stereo area, a 1:1
// port of the non-complex-prediction paths of libAACdec/src/stereo.cpp.
//
// These run after the spectrum of a channel_pair_element has been Huffman-
// decoded and inverse-quantized, transforming the L/R (or mid/side) spectra
// in place per ISO/IEC 14496-3 §4.6.8.1 (M/S) and §4.6.8.2 (intensity). All
// arithmetic here is fixed-point integer (FIXP_DBL == int32, FIXP_SGL ==
// int16); it is bit-identical regardless of vectorization, so none of this is
// FP-gated by aac_strict.
//
// Scope of this area: M/S stereo and intensity stereo. The complex-prediction
// (USAC) branch of CJointStereo_ApplyMS — which needs the MDST filterbank,
// the persistent previous-frame downmix and the cplx-prediction coefficient
// tables — is a separate area and is intentionally not ported here.
// ApplyMS asserts cplx_pred_flag is clear (it is never set on the M/S+IS path).
//
// The INTENSITY_HCB / INTENSITY_HCB2 pseudo-codebook constants this area reads
// (channelinfo.h:188-189) are already defined alongside the spectral-Huffman
// codebooks in block_constants.go and are reused here.

// jointStereoMaximumBands is the MsUsed flag-array length. C counterpart:
// JointStereoMaximumBands, libAACdec/src/stereo.h:130.
const jointStereoMaximumBands = 64

// JointStereoData is the per-frame joint-stereo scratch state read out of the
// bitstream and consumed by the apply tools. Faithful subset of
// CJointStereoData (libAACdec/src/stereo.h:146): only the fields the ms-stereo
// / intensity area touches are modelled.
//
// MsUsed has one entry per scalefactor band; each entry packs up to 8 group
// flags (bit g set => M/S used for that band in window group g).
type JointStereoData struct {
	MsMaskPresent uint8
	MsUsed        [jointStereoMaximumBands]uint8
}

// generateMSOutput performs the M/S upmix for one scalefactor band, in place.
// C counterpart: CJointStereo_GenerateMSOutput, libAACdec/src/stereo.cpp:492.
//
// Each L/R coefficient pair is first brought to a common scale by the
// per-channel right-shifts leftScale/rightScale, then replaced by the
// mid/side sum and difference:
//
//	L' = (L>>leftScale) + (R>>rightScale)
//	R' = (L>>leftScale) - (R>>rightScale)
//
// The C version unrolls by 4 and walks the band backwards; the result is
// identical to the straightforward forward loop below, so it is written
// plainly per the project's "don't preserve incidental unrolling" convention.
func generateMSOutput(specL, specR []int32, leftScale, rightScale uint, nSfbBands int) {
	for i := 0; i < nSfbBands; i++ {
		left := specL[i] >> leftScale
		right := specR[i] >> rightScale
		specL[i] = left + right
		specR[i] = left - right
	}
}

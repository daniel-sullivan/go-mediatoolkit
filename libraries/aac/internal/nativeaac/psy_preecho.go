// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Pre-echo control for the psychoacoustic model — a 1:1 port of
// pre_echo_control.cpp (FDKaacEnc_InitPreEchoControl, line 106;
// FDKaacEnc_PreEchoControl, line 117). FDKaacEnc_psyMain calls PreEchoControl
// once per window after spreading to limit how much a band's masking
// threshold may rise relative to the previous block (carrying the per-channel
// sfbThresholdnm1 / mdctScalenm1 state), preventing pre-echo artifacts. Pure
// fixed-point: FIXP_DBL/FIXP_SGL fractions, fMult, fixMax/fixMin, arithmetic
// shifts — bit-identical regardless of build tag.

// pcmQuantThrScale is PCM_QUANT_THR_SCALE (psy_configuration.h:114).
const pcmQuantThrScale = 16

// maxvalDBLPreEcho is MAXVAL_DBL == 0x7FFFFFFF (common_fix.h:155). Named to
// avoid collision with maxvalDBL in block_switch.go (same value, separate
// citation kept local to this feature file would shadow; reuse the existing
// package const instead).

// initPreEchoControl initializes the pre-echo state for one channel: seeds
// mdctScalenm1 with PCM_QUANT_THR_SCALE/2, copies the PCM quantization
// thresholds into pbThresholdNm1, and arms calcPreEcho. C counterpart:
// FDKaacEnc_InitPreEchoControl, pre_echo_control.cpp:106.
//
//	void FDKaacEnc_InitPreEchoControl(FIXP_DBL *pbThresholdNm1, INT *calcPreEcho,
//	                                  INT numPb, FIXP_DBL *sfbPcmQuantThreshold,
//	                                  INT *mdctScalenm1) {
//	  *mdctScalenm1 = PCM_QUANT_THR_SCALE >> 1;
//	  FDKmemcpy(pbThresholdNm1, sfbPcmQuantThreshold, numPb * sizeof(FIXP_DBL));
//	  *calcPreEcho = 1;
//	}
//
// Returns (mdctScalenm1, calcPreEcho) and writes pbThresholdNm1[0:numPb].
func initPreEchoControl(pbThresholdNm1, sfbPcmQuantThreshold []int32, numPb int) (mdctScalenm1, calcPreEcho int) {
	mdctScalenm1 = pcmQuantThrScale >> 1
	copy(pbThresholdNm1[:numPb], sfbPcmQuantThreshold[:numPb])
	calcPreEcho = 1
	return mdctScalenm1, calcPreEcho
}

// preEchoControl limits the per-band threshold increase from one block to the
// next, updating pbThreshold[0:numPb] and the carried pbThresholdNm1 state in
// place. C counterpart: FDKaacEnc_PreEchoControl, pre_echo_control.cpp:117.
// Returns the updated mdctScalenm1 (the C writes it through *mdctScalenm1).
//
// maxAllowedIncreaseFactor is an INT (not a fraction): the C performs an
// integer multiply against the (right-shifted) previous threshold, which is
// reproduced here as an int32 multiply (the product is interpreted as
// FIXP_DBL exactly as in the C). minRemainingThresholdFactor is a FIXP_SGL.
//
//	void FDKaacEnc_PreEchoControl(FIXP_DBL *pbThresholdNm1, INT calcPreEcho,
//	                              INT numPb, INT maxAllowedIncreaseFactor,
//	                              FIXP_SGL minRemainingThresholdFactor,
//	                              FIXP_DBL *pbThreshold, INT mdctScale,
//	                              INT *mdctScalenm1) { ... }
func preEchoControl(
	pbThresholdNm1 []int32, calcPreEcho, numPb, maxAllowedIncreaseFactor int,
	minRemainingThresholdFactor int16, pbThreshold []int32,
	mdctScale, mdctScalenm1 int,
) int {
	var tmpThreshold1, tmpThreshold2 int32
	var scaling int

	// If lastWindowSequence in previous frame was start- or stop-window,
	// skip preechocontrol calculation.
	if calcPreEcho == 0 {
		// copy thresholds to internal memory
		copy(pbThresholdNm1[:numPb], pbThreshold[:numPb])
		return mdctScale
	}

	if mdctScale > mdctScalenm1 {
		// if current thresholds are downscaled more than the ones from the last block
		scaling = 2 * (mdctScale - mdctScalenm1)
		for i := 0; i < numPb; i++ {
			// multiplication with return data type fract is equivalent to int multiplication
			tmpThreshold1 = int32(maxAllowedIncreaseFactor) * (pbThresholdNm1[i] >> uint(scaling))
			tmpThreshold2 = fMultDS(pbThreshold[i], minRemainingThresholdFactor)

			tmp := pbThreshold[i]

			// copy thresholds to internal memory
			pbThresholdNm1[i] = tmp

			tmp = fixMinDBL(tmp, tmpThreshold1)
			pbThreshold[i] = fixMaxDBL(tmp, tmpThreshold2)
		}
	} else {
		// if thresholds of last block are more downscaled than the current ones
		scaling = 2 * (mdctScalenm1 - mdctScale)
		for i := 0; i < numPb; i++ {
			tmpThreshold1 = int32(maxAllowedIncreaseFactor>>1) * pbThresholdNm1[i]
			tmpThreshold2 = fMultDS(pbThreshold[i], minRemainingThresholdFactor)

			// copy thresholds to internal memory
			pbThresholdNm1[i] = pbThreshold[i]

			if (pbThreshold[i] >> uint(scaling+1)) > tmpThreshold1 {
				pbThreshold[i] = tmpThreshold1 << uint(scaling+1)
			}
			pbThreshold[i] = fixMaxDBL(pbThreshold[i], tmpThreshold2)
		}
	}

	return mdctScale
}

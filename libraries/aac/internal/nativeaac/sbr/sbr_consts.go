// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// SBR decoder limit constants, 1:1 from the vendored libSBRdec headers. Only the
// HE-AAC v1 (STD, no PS / no USAC-HBE / no ELD) subset is named; the
// PS/USAC/ELD-only sizes are out of this batch's scope.
const (
	maxNoiseEnvelopes    = 2                                  // MAX_NOISE_ENVELOPES (lpp_tran.h:127)
	maxNoiseCoeffs       = 5                                  // MAX_NOISE_COEFFS (lpp_tran.h:128)
	maxNumNoiseValues    = maxNoiseEnvelopes * maxNoiseCoeffs // MAX_NUM_NOISE_VALUES (lpp_tran.h:129)
	maxNumLimiters       = 12                                 // MAX_NUM_LIMITERS (lpp_tran.h:130)
	maxEnvelopes         = 8                                  // MAX_ENVELOPES == MAX_ENVELOPES_USAC (lpp_tran.h:136)
	maxFreqCoeffs        = 56                                 // MAX_FREQ_COEFFS == MAX_FREQ_COEFFS_QUAD_RATE (lpp_tran.h:140)
	maxNumEnvelopeValues = maxEnvelopes * maxFreqCoeffs       // MAX_NUM_ENVELOPE_VALUES (lpp_tran.h:145)
	maxInvfBands         = maxNoiseCoeffs                     // MAX_INVF_BANDS (lpp_tran.h:159)
	maxPvcEnvelopes      = 2                                  // MAX_PVC_ENVELOPES (pvc_dec.h:113)
	pvcNTimeslot         = 16                                 // PVC_NTIMESLOT (pvc_dec.h:114)
	addHarmonicsFlagsSz  = 2                                  // ADD_HARMONICS_FLAGS_SIZE (env_extr.h:159)
)

// Envelope pseudo-float pack constants, 1:1 from env_extr.h:119-157. The
// iEnvelope[]/sbrNoiseFloorLevel[] arrays store a 6-bit exponent in the low bits
// and a (FRACT_BITS-EXP_BITS)-bit mantissa above it.
const (
	envExpFract  = 0 // ENV_EXP_FRACT (env_extr.h:119)
	expBits      = 6 // EXP_BITS (env_extr.h:126)
	maskE        = (1 << expBits) - 1
	nrgExpOffset = 16 // NRG_EXP_OFFSET (env_extr.h:152)
	noiseExpOff  = 38 // NOISE_EXP_OFFSET (env_extr.h:155)
)

// SBR_SYNC_STATE values, 1:1 from env_extr.h:168-173.
const (
	sbrNotInitialized = 0
	upsampling        = 1
	sbrHeaderState    = 2
	sbrActive         = 3
)

// SBR_HEADER_STATUS values, 1:1 from env_extr.h:161-166.
const (
	headerNotPresent = 0
	headerError      = 1
	headerOK         = 2
	headerReset      = 3
)

// COUPLING_MODE values, 1:1 from env_extr.h:175.
const (
	couplingOff   = 0
	couplingLevel = 1
	couplingBal   = 2
)

// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Parametric-stereo (HE-AAC v2) ENCODE-side constants, ROM tables and structs,
// ported 1:1 from the Fraunhofer FDK-AAC SBR encoder. These mirror the encoder
// declarations in libSBRenc/src/ps_const.h, ps_encode.h, ps_bitenc.h and
// ps_main.h; they are DISTINCT from the PS DECODE structs (ps_types.go /
// psdec.h) — the encoder carries its own PS_DATA / PS_ENCODE / PS_OUT layout.
//
// FDK-AAC-derived; see libfdk/COPYING. Fenced behind the aacfdk build tag.
// FIXED-POINT => byte-identical / exact-integer parity (no FP/strict axis).
// GA baseline HE-AAC v2 only: IPD/OPD not transmitted, DRM/LD/ELD/USAC excluded.
package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// PS encode constants (ps_const.h:106-148).
const (
	maxPsChannels      = 2  // MAX_PS_CHANNELS
	hybridMaxQmfBands  = 3  // HYBRID_MAX_QMF_BANDS
	hybridFilterLength = 13 // HYBRID_FILTER_LENGTH
	hybridFilterDelayE = (hybridFilterLength - 1) / 2
	hybridFramesize    = 32 // HYBRID_FRAMESIZE
	hybridReadOffset   = 10 // HYBRID_READ_OFFSET
	maxHybridBandsE    = (64 - hybridMaxQmfBands + 10)
	psMaxEnvelopesE    = 4          // PS_MAX_ENVELOPES
	psBandsCoarse      = 10         // PS_BANDS_COARSE
	psBandsMid         = 20         // PS_BANDS_MID
	psMaxBandsE        = psBandsMid // PS_MAX_BANDS
	psIidResCoarse     = 0          // PS_IID_RES_COARSE
	psIidResFine       = 1          // PS_IID_RES_FINE
	psIccRotA          = 0          // PS_ICC_ROT_A
	psIccRotB          = 1          // PS_ICC_ROT_B
	psDeltaFreq        = 0          // PS_DELTA_FREQ
	psDeltaTime        = 1          // PS_DELTA_TIME
	psResCoarse        = 0          // PS_RES_COARSE
	psResMid           = 1          // PS_RES_MID
	psResFine          = 2          // PS_RES_FINE
)

// FDK_PSENC_ERROR codes (ps_const.h:139-148).
const (
	psencOK            = 0x0000
	psencInvalidHandle = 0x0020
	psencMemoryError   = 0x0021
	psencInitError     = 0x0040
	psencEncodeError   = 0x0060
)

// ps_encode.h:114-126.
const (
	iidScaleFt     = 64.0 // IID_SCALE_FT
	iidScale       = 6    // IID_SCALE
	psQuantScale   = 6    // PS_QUANT_SCALE
	qmfGroupsLoRes = 12   // QMF_GROUPS_LO_RES
	subqmfGroupsLo = 10   // SUBQMF_GROUPS_LO_RES
	qmfGroupsHiRes = 18   // QMF_GROUPS_HI_RES
	subqmfGroupsHi = 30   // SUBQMF_GROUPS_HI_RES
)

// ps_encode.cpp:140-145 (__PS_CONSTANTS).
const (
	maxTimeDiffFrames = 20
	maxPsNoHeaderCnt  = 10
	maxNoEnvCnt       = 10
	doNotUseThisMode  = 0x7FFFFF
)

const threshScale = 7 // ps_encode.cpp:280 THRESH_SCALE

// ldDataShift is LD_DATA_SHIFT (== 6). DFRACT_BITS (== 32) is provided by the
// package as dfractBits (qmf_synthesis.go).
const ldDataShift = 6

const log102_10 = 3.01029995664 // LOG10_2_10 (ps_encode.cpp:121)

// iidQuant_fx, iidQuantFine_fx, iccQuant (ps_encode.cpp:147-170). Hex literals
// reproduced exactly as int32.
var iidQuantFx = [15]int32{
	int32(-0x32000000), int32(-0x24000000), int32(-0x1c000000),
	int32(-0x14000000), int32(-0x0e000000), int32(-0x08000000),
	int32(-0x04000000), int32(0x00000000), int32(0x04000000),
	int32(0x08000000), int32(0x0e000000), int32(0x14000000),
	int32(0x1c000000), int32(0x24000000), int32(0x32000000),
}

var iidQuantFineFx = [31]int32{
	int32(-0x63ffffff), int32(-0x59ffffff), int32(-0x4fffffff),
	int32(-0x45ffffff), int32(-0x3c000000), int32(-0x32000000),
	int32(-0x2c000000), int32(-0x26000000), int32(-0x20000000),
	int32(-0x1a000000), int32(-0x14000000), int32(-0x10000000),
	int32(-0x0c000000), int32(-0x08000000), int32(-0x04000000),
	int32(0x00000000), int32(0x04000000), int32(0x08000000),
	int32(0x0c000000), int32(0x10000000), int32(0x14000000),
	int32(0x1a000000), int32(0x20000000), int32(0x26000000),
	int32(0x2c000000), int32(0x32000000), int32(0x3c000000),
	int32(0x45ffffff), int32(0x4fffffff), int32(0x59ffffff),
	int32(0x63ffffff),
}

var iccQuant = [8]int32{
	int32(0x7fffffff), int32(0x77ef9d7f), int32(0x6babc97f),
	int32(0x4ceaf27f), int32(0x2f0ed3c0), int32(0x00000000),
	int32(-0x4b6459ff), int32(-0x80000000),
}

// iidGroupBordersLoRes (ps_encode.cpp:123-128).
var iidGroupBordersLoRes = [qmfGroupsLoRes + subqmfGroupsLo + 1]int32{
	0, 1, 2, 3, 4, 5,
	6, 7,
	8, 9,
	10, 11, 12, 13, 14, 15, 16, 18, 21, 25, 30, 42, 71,
}

// iidGroupWidthLdLoRes (ps_encode.cpp:130-132).
var iidGroupWidthLdLoRes = [qmfGroupsLoRes + subqmfGroupsLo]uint8{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 2, 2, 3, 4, 5,
}

// subband2parameter20 (ps_encode.cpp:134-138).
var subband2parameter20 = [qmfGroupsLoRes + subqmfGroupsLo]int32{
	1, 0, 0, 1, 2, 3,
	4, 5,
	6, 7,
	8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
}

// psData is the 1:1 port of T_PS_DATA (ps_encode.h:128-152).
type psData struct {
	iidEnable        int
	iidEnableLast    int
	iidQuantMode     int
	iidQuantModeLast int
	iidDiffMode      [psMaxEnvelopesE]int
	iidIdx           [psMaxEnvelopesE][psMaxBandsE]int
	iidIdxLast       [psMaxBandsE]int

	iccEnable        int
	iccEnableLast    int
	iccQuantMode     int
	iccQuantModeLast int
	iccDiffMode      [psMaxEnvelopesE]int
	iccIdx           [psMaxEnvelopesE][psMaxBandsE]int
	iccIdxLast       [psMaxBandsE]int

	nEnvelopesLast int

	headerCnt  int
	iidTimeCnt int
	iccTimeCnt int
	noEnvCnt   int
}

// psEncode is the 1:1 port of T_PS_ENCODE (ps_encode.h:154-167).
type psEncode struct {
	psData psData

	psEncMode              int // PS_BANDS
	nQmfIidGroups          int
	nSubQmfIidGroups       int
	iidGroupBorders        [qmfGroupsHiRes + subqmfGroupsHi + 1]int32
	subband2parameterIndex [qmfGroupsHiRes + subqmfGroupsHi]int32
	iidGroupWidthLd        [qmfGroupsHiRes + subqmfGroupsHi]uint8
	iidQuantErrorThreshold int32

	psBandNrgScale [psMaxBandsE]int8
}

// psOut is the 1:1 port of T_PS_OUT (ps_bitenc.h:110-143). Only the IID/ICC
// fields are populated by the GA baseline encoder; IPD/OPD stay zero.
type psOut struct {
	enablePSHeader int
	enableIID      int
	iidMode        int
	enableICC      int
	iccMode        int
	enableIpdOpd   int

	frameClass  int
	nEnvelopes  int
	frameBorder [psMaxEnvelopesE]int

	deltaIID [psMaxEnvelopesE]int // PS_DELTA
	iid      [psMaxEnvelopesE][psMaxBandsE]int
	iidLast  [psMaxBandsE]int

	deltaICC [psMaxEnvelopesE]int // PS_DELTA
	icc      [psMaxEnvelopesE][psMaxBandsE]int
	iccLast  [psMaxBandsE]int

	deltaIPD [psMaxEnvelopesE]int
	ipd      [psMaxEnvelopesE][psMaxBandsE]int
	ipdLast  [psMaxBandsE]int

	deltaOPD [psMaxEnvelopesE]int
	opd      [psMaxEnvelopesE][psMaxBandsE]int
	opdLast  [psMaxBandsE]int
}

// --- package-local fixed-point helpers used by the PS encode kernels --------
// (the sbr package already provides fMult, fMultDiv2, fAbs, cntLeadingZeros,
// fl2fxconstDBL; these add the remainder used only by the PS encoder.)

func fixMaxI32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func fixMinI32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func fMaxPS(a, b int32) int32                    { return nativeaac.FMaxDBL(a, b) } // fMax(DBL,DBL)
func fPow2Div2PS(a int32) int32                  { return nativeaac.FPow2Div2(a) }
func ldDataVectorPS(src, dst []int32, n int)     { nativeaac.LdDataVector(src, dst, n) }
func invSqrtNorm2PS(op int32) (int32, int32)     { return nativeaac.InvSqrtNorm2(op) }
func fDivNormPS(num, denom int32) (int32, int32) { return nativeaac.FDivNorm(num, denom) }
func fMultNormPS(f1, f2 int32) (int32, int32)    { return nativeaac.FMultNorm(f1, f2) }
func sqrtFixpPS(op int32) int32                  { return nativeaac.SqrtFixp(op) }
func schurDivPS(num, denum, count int32) int32   { return nativeaac.SchurDiv(num, denum, count) }
func getInvIntPS(v int) int32                    { return nativeaac.GetInvInt(v) }
func fMultIPS(a, b int32) int32                  { return nativeaac.FMultI(a, b) }
func scaleValueSaturatePS(value, scalefactor int32) int32 {
	return nativeaac.ScaleValueSaturate(value, scalefactor)
}
func countLeadingBitsPS(x int32) int { return nativeaac.CountLeadingBits(x) }

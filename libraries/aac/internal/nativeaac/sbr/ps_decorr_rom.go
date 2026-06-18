// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Decorrelator ROM tables, ported 1:1 from the vendored Fraunhofer FDK-AAC
// FDK_decorrelate.cpp. Only the HE-AAC v2 baseline PS (DECORR_PS, !partiallyComplex,
// isLegacyPS) tables are needed: the packed complex allpass coefficients
// (DecorrPsCoeffsCplx), the PS reverb-band layout (REV_*_PS_HQ), the
// stateBufferOffset init, and the 20<->71 ducker band maps. The MPS/USAC/LD
// numerator tables are NOT ported (unreachable in the PS path).
//
// FIXED-POINT / arch convention: __ARM_ARCH_8__ => SINETABLE_16BIT (STCP packs
// two Q1.15 narrowed values) and !ARCH_PREFER_MULT_32x32 => FIXP_DUCK_GAIN is
// FIXP_SGL, so the duck constants are Q1.15.

// decorrFilterOrderPs is DECORR_FILTER_ORDER_PS (FDK_decorrelate.cpp:136).
const decorrFilterOrderPs = 12

// duckerMaxNrgScale / duckerHeadroomBits / filterSf (FDK_decorrelate.cpp:229-232).
const (
	duckerMaxNrgScale  = 24
	duckerHeadroomBits = 2
	filterSf           = 2
)

// PS reverb-band layout (HQ, !partiallyComplex).
//
// C: REV_bandOffset_PS_HQ / REV_delay_PS_HQ / REV_filterOrder_PS / REV_filtType_PS
// (FDK_decorrelate.cpp:147,159,161,174) and stateBufferOffsetInit (:179).
var (
	revBandOffsetPsHQ     = [4]uint8{30, 42, 71, 71}
	revDelayPsHQ          = [4]uint8{2, 14, 1, 0}
	revFilterOrderPs      = [4]int8{decorrFilterOrderPs, -1, -1, -1}
	revFiltTypePs         = [4]revbandFiltType{indepCplxPs, delayBand, delayBand, notExist}
	stateBufferOffsetInit = [3]uint8{0, 6, 14}
)

// kernels20To71Ps maps each of the 71 hybrid bands to its 20 stereo (param) band.
//
// C: kernels_20_to_71_PS (FDK_decorrelate.cpp:186).
var kernels20To71Ps = [71 + 1]uint8{
	0, 0, 1, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 14,
	15, 15, 15, 16, 16, 16, 16, 17, 17, 17, 17, 17, 18, 18, 18, 18, 18, 18,
	18, 18, 18, 18, 18, 18, 19, 19, 19, 19, 19, 19, 19, 19, 19, 19, 19, 19,
	19, 19, 19, 19, 19, 19, 19, 19, 19, 19, 19, 19, 19, 19, 19, 19, 19, 19,
}

// kernels20To71OffsetPs maps each of the 20 param bands to its first hybrid band.
//
// C: kernels_20_to_71_offset_PS (FDK_decorrelate.cpp:194).
var kernels20To71OffsetPs = [20 + 1]uint8{
	0, 2, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 18, 21, 25, 30, 42, 71,
}

// duck constants. FIXP_DUCK_GAIN == FIXP_SGL on this target.
//
// C: PS_DUCK_PEAK_DECAY_FACTOR_FDK / PS_DUCK_FILTER_COEFF_FDK (FDK_decorrelate.cpp:
// 248-249) and the inline FL2FXCONST_DUCK(0.75f) / (2/3f) constants in DuckerApplyPS.
var (
	psDuckPeakDecayFactor = nativeaac.Fl2fxconstSGLf(0.765928338364649)
	psDuckFilterCoeff     = nativeaac.Fl2fxconstSGLf(0.25)
	duck0p75              = nativeaac.Fl2fxconstSGLf(0.75)
	duck2div3             = nativeaac.Fl2fxconstSGLf(2.0 / 3.0)
)

// absThrDenomBias is FL2FXCONST_DBL(ABS_THR / (32768.0f*32768.0f*128.0f*1.5f))
// (DuckerApplyPS), the small bias added to peakDiff before invSqrtNorm2. ABS_THR
// == 1e-9f*32768*32768. The whole argument is a single-precision (float) constant
// expression in the C: 1e-9f is float, all the divisor literals are .0f, so the
// arithmetic is done in float32 and only then widened by FL2FXCONST_DBL's
// (double) cast. Replicated here with explicit float32 ops, then Fl2fxconstDBLf.
var absThrDenomBias = func() int32 {
	absThr := float32(1e-9) * 32768 * 32768
	denom := float32(32768.0) * 32768.0 * 128.0 * 1.5
	return nativeaac.Fl2fxconstDBLf(absThr / denom)
}()

// decorrPsCoeffsCplx is DecorrPsCoeffsCplx[][4] (FDK_decorrelate.cpp:251): the
// packed complex allpass coefficients for the 30 hybrid bands of reverb band 0,
// 4 per band (Phi(k) multiplicant + 3 stage coeffs). STCP(cos,sin) narrows two
// Q31 hex literals to Q1.15 via STC == FX_DBL2FXCONST_SGL.
var decorrPsCoeffsCplx = [30][4]fixSTP{
	{stcp(0x5d6940eb, 0x5783153e), stcp(0xadcd41a8, 0x0e0373ed), stcp(0xbad41f3e, 0x14fba045), stcp(0xc1eb6694, 0x0883227d)},
	{stcp(0x5d6940eb, 0xa87ceac2), stcp(0xadcd41a8, 0xf1fc8c13), stcp(0xbad41f3e, 0xeb045fbb), stcp(0xc1eb6694, 0xf77cdd83)},
	{stcp(0xaec24162, 0x62e9d75b), stcp(0xb7169316, 0x28751048), stcp(0xd224c0cc, 0x37e05050), stcp(0xc680864f, 0x18e88cba)},
	{stcp(0xaec24162, 0x9d1628a5), stcp(0xb7169316, 0xd78aefb8), stcp(0xd224c0cc, 0xc81fafb0), stcp(0xc680864f, 0xe7177346)},
	{stcp(0x98012341, 0x4aa00ed1), stcp(0xc89ca1b2, 0xc1ab6bff), stcp(0xf8ea394e, 0xb8106bf4), stcp(0xcf542d73, 0xd888b99b)},
	{stcp(0x43b137b3, 0x6ca2ca40), stcp(0xe0649cc4, 0xb2d69cca), stcp(0x22130c21, 0xc0405382), stcp(0xdbbf8fba, 0xcce3c7cc)},
	{stcp(0x28fc4d71, 0x86bd3b87), stcp(0x09ccfeb9, 0xad319baf), stcp(0x46e51f02, 0xf1e5ea55), stcp(0xf30d5e34, 0xc2b0e335)},
	{stcp(0xc798f756, 0x72e73c7d), stcp(0x3b6c3c1e, 0xc580dc72), stcp(0x2828a6ba, 0x3c1a14fb), stcp(0x14b733bb, 0xc4dcaae1)},
	{stcp(0x46dcadd3, 0x956795c7), stcp(0x52f32fae, 0xf78048cd), stcp(0xd7d75946, 0x3c1a14fb), stcp(0x306017cb, 0xd82c0a75)},
	{stcp(0xabe197de, 0x607a675e), stcp(0x460cef6e, 0x2d3b264e), stcp(0xb91ae0fe, 0xf1e5ea55), stcp(0x3e03e5e0, 0xf706590e)},
	{stcp(0xb1b4f509, 0x9abcaf5f), stcp(0xfeb0b4be, 0x535fb8ba), stcp(0x1ba96f8e, 0xbd37e6d8), stcp(0x30f6dbbb, 0x271a0743)},
	{stcp(0xce75b52a, 0x89f9be61), stcp(0xb26e4dda, 0x101054c5), stcp(0x1a475d2e, 0x3f714b19), stcp(0xf491f154, 0x3a6baf46)},
	{stcp(0xee8fdfcb, 0x813181fa), stcp(0xe11e1a00, 0xbb9a6039), stcp(0xc3e582f5, 0xe71ab533), stcp(0xc9eb35e2, 0x0ffd212a)},
	{stcp(0x0fd7d92f, 0x80fbf975), stcp(0x38adccbc, 0xd571bbf4), stcp(0x38c3aefc, 0xe87cc794), stcp(0xdafe8c3d, 0xd9b16100)},
	{stcp(0x300d9e10, 0x895cc359), stcp(0x32b9843e, 0x2b52adcc), stcp(0xe9ded9f4, 0x356ce0ed), stcp(0x0fdd5ca3, 0xd072932e)},
	{stcp(0x4d03b4f8, 0x99c2dec3), stcp(0xe2bc8d94, 0x3744e195), stcp(0xeb40ec55, 0xcde9ed22), stcp(0x2e67e231, 0xf893470b)},
	{stcp(0x64c4deb3, 0xb112790f), stcp(0xc7b32682, 0xf099172d), stcp(0x2ebf44cf, 0x135d014a), stcp(0x1a2bacd5, 0x23334254)},
	{stcp(0x75b5f9aa, 0xcdb81e14), stcp(0x028d9bb1, 0xc9dc45b9), stcp(0xd497893f, 0x11faeee9), stcp(0xee40ff71, 0x24a91b85)},
	{stcp(0x7eb1cd81, 0xedc3feec), stcp(0x31491897, 0xf765f6d8), stcp(0x1098dc89, 0xd7ee574e), stcp(0xda6b816d, 0x011f35cf)},
	{stcp(0x7f1cde01, 0x0f0b7727), stcp(0x118ce49d, 0x2a5ecda4), stcp(0x0f36ca28, 0x24badaa3), stcp(0xef2908a4, 0xe1ee3743)},
	{stcp(0x76efee25, 0x2f4e8c3a), stcp(0xdde3be2a, 0x17f92215), stcp(0xde9bf36c, 0xf22b4839), stcp(0x1128fc0c, 0xe5c95f5a)},
	{stcp(0x66b87d65, 0x4c5ede42), stcp(0xe43f351a, 0xe6bf22dc), stcp(0x1e0d3e85, 0xf38d5a9a), stcp(0x1c0f44a3, 0x02c92fe3)},
	{stcp(0x4f8f36b7, 0x6445680f), stcp(0x10867ea2, 0xe3072740), stcp(0xf4ef6cfa, 0x1ab67076), stcp(0x09562a8a, 0x1742bb8b)},
	{stcp(0x3304f6ec, 0x7564812a), stcp(0x1be4f1a8, 0x0894d75a), stcp(0xf6517f5b, 0xe8a05d98), stcp(0xf1bb0053, 0x10a78853)},
	{stcp(0x1307b2c5, 0x7e93d532), stcp(0xfe098e27, 0x18f02a58), stcp(0x1408d459, 0x084c6e44), stcp(0xedafe5bd, 0xfbc15b2e)},
	{stcp(0xf1c111cd, 0x7f346c97), stcp(0xeb5ca6a0, 0x02efee93), stcp(0xef4df9b6, 0x06ea5be4), stcp(0xfc149289, 0xf0d53ce4)},
	{stcp(0xd1710001, 0x773b6beb), stcp(0xfa1aeb8c, 0xf06655ff), stcp(0x05884983, 0xf2a4c7c5), stcp(0x094f13df, 0xf79c01bf)},
	{stcp(0xb446be0b, 0x6732cfca), stcp(0x0a743752, 0xf9220dfa), stcp(0x04263722, 0x0a046a2c), stcp(0x08ced80b, 0x0347e9c2)},
	{stcp(0x9c3b1202, 0x503018a5), stcp(0x05fcf01a, 0x05cd8529), stcp(0xf95263e2, 0xfd3bdb3f), stcp(0x00c68cf9, 0x0637cb7f)},
	{stcp(0x8aee2710, 0x33c187ec), stcp(0xfdd253f8, 0x038e09b9), stcp(0x0356ce0f, 0xfe9ded9f), stcp(0xfd6c3054, 0x01c8060a)},
}

// stcp is STCP(cos, sin): narrow two Q31 hex literals to Q1.15 via STC ==
// FX_DBL2FXCONST_SGL.
func stcp(cos, sin uint32) fixSTP {
	return fixSTP{re: nativeaac.StcNarrow(int32(cos)), im: nativeaac.StcNarrow(int32(sin))}
}

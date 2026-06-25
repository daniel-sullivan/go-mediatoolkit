// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// SBR decoder constant tables, a 1:1 port of the vendored libSBRdec sbr_rom.cpp
// (FDK_sbrDecoder_* symbols). Scope is HE-AAC v1 (STD) decode: the start/stop
// band tables, the envelope-adjustor gain/smoothing/randomPhase tables, the
// envelope-extractor frame-info defaults + envelope tables, the SBR Huffman
// codebooks, and the 1/x lookup table.
//
// EXCLUDED (out of HE-AAC v1 scope, flagged for later batches):
//   - the parametric-stereo (HE-AAC v2 / PS) codebooks and tables
//     (aBookPsIid*, aBookPsIcc*, ScaleFactors*, Alphas, bins2groupMap20,
//     aNoIidBins/aNoIccBins, aFixNoEnvDecode) — psdec/psbitdec only.
//   - the LP-transposer whitening table (whFactorsTable/whFactorsIndex) — that
//     is consumed by the HF-generation (lpp_tran) batch, not the
//     bitstream/envelope batch; ported there.
//
// The FL2FXCONST_SGL / FL2FXCONST_DBL float-literal tables are materialised once
// in init() through nativeaac.Fl2fxconstSGL/DBL — the same bit-exact macro the C
// compiler folds — mirroring the AAC-LC ROM pattern; the parity oracle asserts
// the result is byte-identical to the genuine in-RAM C symbol. The integer-hex
// tables are stored verbatim.

// --- StartStopBands (sbr_rom.cpp:123-155) -----------------------------------
// startFreqXX[k][16]: start/stop subbands of the highband, indexed by
// [k==0:start, k==1:offset for stop] then bs_start_freq. The single-row _192 /
// _176 / _128 tables are the SBRDEC_RATIO_16_64 stop-band variants.

var sbrStartFreq16 = [2][16]uint8{
	{16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31},
	{4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}}
var sbrStartFreq22 = [2][16]uint8{
	{12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 26, 28, 30},
	{4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 18, 20, 22}}
var sbrStartFreq24 = [2][16]uint8{
	{11, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 25, 27, 29, 32},
	{3, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 17, 19, 21, 24}}
var sbrStartFreq32 = [2][16]uint8{
	{10, 12, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 25, 27, 29, 32},
	{2, 4, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 17, 19, 21, 24}}
var sbrStartFreq40 = [2][16]uint8{
	{12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 24, 26, 28, 30, 32},
	{5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 17, 19, 21, 23, 25}}
var sbrStartFreq44 = [2][16]uint8{
	{8, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 21, 23, 25, 28, 32},
	{2, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 15, 17, 19, 22, 26}}
var sbrStartFreq48 = [2][16]uint8{
	{7, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 20, 22, 24, 27, 31},
	{1, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16, 18, 21, 25}}
var sbrStartFreq64 = [2][16]uint8{
	{6, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 19, 21, 23, 26, 30},
	{1, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16, 18, 21, 25}}
var sbrStartFreq88 = [2][16]uint8{
	{5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 16, 18, 20, 23, 27, 31},
	{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 20, 24, 28}}
var sbrStartFreq192 = [16]uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 12, 14, 16, 19, 23, 27}
var sbrStartFreq176 = [16]uint8{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 20, 24, 28}
var sbrStartFreq128 = [16]uint8{1, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16, 18, 21, 25}

// --- EnvAdj gain/smoothing/randomPhase (sbr_rom.cpp:209-757) -----------------

// sbrLimGainsM/E are the mantissas/exponents of the four gain limits
// (limiterGains 0..3). Mantissas are FL2FXCONST_SGL, narrowed in init().
// The ROM float literals in sbr_rom.cpp are all f-suffixed, so each is rounded
// to float32 before the FL2FXCONST scaling — materialised via the f-form helpers.
var sbrLimGainsMFloat = [4]float32{0.5011932025, 0.5, 0.9976346258, 0.6776263578}
var sbrLimGainsM [4]int16
var sbrLimGainsE = [4]uint8{0, 1, 1, 67} // sbr_rom.cpp:221

// sbrLimiterBandsPerOctaveDiv4 (FIXP_SGL) and _DBL (FIXP_DBL): limiter-band count
// constants (sbr_rom.cpp:224-231).
// f-suffixed in C: FL2FXCONST_*(1.2f / 4.0f) etc., so the float32 division
// (0.3 -> 0.30000001192...) is what gets scaled — not the float64 0.3.
var sbrLimiterBPODiv4Float = [4]float32{1.0 / 4.0, 1.2 / 4.0, 2.0 / 4.0, 3.0 / 4.0}
var sbrLimiterBPODiv4 [4]int16
var sbrLimiterBPODiv4DBL [4]int32

// sbrSmoothFilter: old-gain ratios for the first 4 timeslots of an envelope
// (sbr_rom.cpp:235-237), FL2FXCONST_SGL.
var sbrSmoothFilterFloat = [4]float32{
	0.66666666666666, 0.36516383427084, 0.14699433520835, 0.03183050093751}
var sbrSmoothFilter [4]int16

// sbrRandomPhase: 512 (re,im) noise pairs, FL2FXCONST_SGL, modulated to the
// desired noise level (sbr_rom.cpp:243-757). Entry [265].im is MAXVAL_SGL.
var sbrRandomPhase [sbrNFNoRandomVal][2]int16

const sbrNFNoRandomVal = 512 // SBR_NF_NO_RANDOM_VAL (sbr_rom.h:115)

// --- FrameInfo defaults (sbr_rom.cpp:829-874) -------------------------------
// Predefined envelope positions for the FIX-FIX case (static framing). The C
// aggregate initializer order is {frameClass, nEnvelopes, borders[9],
// freqRes[8], tranEnv, nNoiseEnvelopes, bordersNoise[3], pvcBorders[3],
// noisePosition, varLength}. The _15 set targets 15 timeslots, _16 16 timeslots.
var sbrFrameInfo1_15 = FrameInfo{FrameClass: 0, NEnvelopes: 1, Borders: [9]uint8{0, 15}, FreqRes: [8]uint8{1}, TranEnv: -1, NNoiseEnv: 1, BordersNoise: [3]uint8{0, 15}}
var sbrFrameInfo2_15 = FrameInfo{FrameClass: 0, NEnvelopes: 2, Borders: [9]uint8{0, 8, 15}, FreqRes: [8]uint8{1, 1}, TranEnv: -1, NNoiseEnv: 2, BordersNoise: [3]uint8{0, 8, 15}}
var sbrFrameInfo4_15 = FrameInfo{FrameClass: 0, NEnvelopes: 4, Borders: [9]uint8{0, 4, 8, 12, 15}, FreqRes: [8]uint8{1, 1, 1, 1}, TranEnv: -1, NNoiseEnv: 2, BordersNoise: [3]uint8{0, 8, 15}}
var sbrFrameInfo8_15 = FrameInfo{FrameClass: 0, NEnvelopes: 8, Borders: [9]uint8{0, 2, 4, 6, 8, 10, 12, 14, 15}, FreqRes: [8]uint8{1, 1, 1, 1, 1, 1, 1, 1}, TranEnv: -1, NNoiseEnv: 2, BordersNoise: [3]uint8{0, 8, 15}}
var sbrFrameInfo1_16 = FrameInfo{FrameClass: 0, NEnvelopes: 1, Borders: [9]uint8{0, 16}, FreqRes: [8]uint8{1}, TranEnv: -1, NNoiseEnv: 1, BordersNoise: [3]uint8{0, 16}}
var sbrFrameInfo2_16 = FrameInfo{FrameClass: 0, NEnvelopes: 2, Borders: [9]uint8{0, 8, 16}, FreqRes: [8]uint8{1, 1}, TranEnv: -1, NNoiseEnv: 2, BordersNoise: [3]uint8{0, 8, 16}}
var sbrFrameInfo4_16 = FrameInfo{FrameClass: 0, NEnvelopes: 4, Borders: [9]uint8{0, 4, 8, 12, 16}, FreqRes: [8]uint8{1, 1, 1, 1}, TranEnv: -1, NNoiseEnv: 2, BordersNoise: [3]uint8{0, 8, 16}}
var sbrFrameInfo8_16 = FrameInfo{FrameClass: 0, NEnvelopes: 8, Borders: [9]uint8{0, 2, 4, 6, 8, 10, 12, 14, 16}, FreqRes: [8]uint8{1, 1, 1, 1, 1, 1, 1, 1}, TranEnv: -1, NNoiseEnv: 2, BordersNoise: [3]uint8{0, 8, 16}}

// --- 1/x lookup table (sbr_rom.cpp:1164) ------------------------------------
const invTableBits = 8                 // INV_TABLE_BITS (sbr_rom.h:212)
const invTableSize = 1 << invTableBits // INV_TABLE_SIZE

var sbrInvTable = [invTableSize]int16{
	0x7f80, 0x7f01, 0x7e83, 0x7e07, 0x7d8b, 0x7d11, 0x7c97, 0x7c1e, 0x7ba6,
	0x7b2f, 0x7ab9, 0x7a44, 0x79cf, 0x795c, 0x78e9, 0x7878, 0x7807, 0x7796,
	0x7727, 0x76b9, 0x764b, 0x75de, 0x7572, 0x7506, 0x749c, 0x7432, 0x73c9,
	0x7360, 0x72f9, 0x7292, 0x722c, 0x71c6, 0x7161, 0x70fd, 0x709a, 0x7037,
	0x6fd5, 0x6f74, 0x6f13, 0x6eb3, 0x6e54, 0x6df5, 0x6d97, 0x6d39, 0x6cdc,
	0x6c80, 0x6c24, 0x6bc9, 0x6b6f, 0x6b15, 0x6abc, 0x6a63, 0x6a0b, 0x69b3,
	0x695c, 0x6906, 0x68b0, 0x685a, 0x6806, 0x67b1, 0x675e, 0x670a, 0x66b8,
	0x6666, 0x6614, 0x65c3, 0x6572, 0x6522, 0x64d2, 0x6483, 0x6434, 0x63e6,
	0x6399, 0x634b, 0x62fe, 0x62b2, 0x6266, 0x621b, 0x61d0, 0x6185, 0x613b,
	0x60f2, 0x60a8, 0x6060, 0x6017, 0x5fcf, 0x5f88, 0x5f41, 0x5efa, 0x5eb4,
	0x5e6e, 0x5e28, 0x5de3, 0x5d9f, 0x5d5a, 0x5d17, 0x5cd3, 0x5c90, 0x5c4d,
	0x5c0b, 0x5bc9, 0x5b87, 0x5b46, 0x5b05, 0x5ac4, 0x5a84, 0x5a44, 0x5a05,
	0x59c6, 0x5987, 0x5949, 0x590a, 0x58cd, 0x588f, 0x5852, 0x5815, 0x57d9,
	0x579d, 0x5761, 0x5725, 0x56ea, 0x56af, 0x5675, 0x563b, 0x5601, 0x55c7,
	0x558e, 0x5555, 0x551c, 0x54e3, 0x54ab, 0x5473, 0x543c, 0x5405, 0x53ce,
	0x5397, 0x5360, 0x532a, 0x52f4, 0x52bf, 0x5289, 0x5254, 0x521f, 0x51eb,
	0x51b7, 0x5183, 0x514f, 0x511b, 0x50e8, 0x50b5, 0x5082, 0x5050, 0x501d,
	0x4feb, 0x4fba, 0x4f88, 0x4f57, 0x4f26, 0x4ef5, 0x4ec4, 0x4e94, 0x4e64,
	0x4e34, 0x4e04, 0x4dd5, 0x4da6, 0x4d77, 0x4d48, 0x4d19, 0x4ceb, 0x4cbd,
	0x4c8f, 0x4c61, 0x4c34, 0x4c07, 0x4bd9, 0x4bad, 0x4b80, 0x4b54, 0x4b27,
	0x4afb, 0x4acf, 0x4aa4, 0x4a78, 0x4a4d, 0x4a22, 0x49f7, 0x49cd, 0x49a2,
	0x4978, 0x494e, 0x4924, 0x48fa, 0x48d1, 0x48a7, 0x487e, 0x4855, 0x482d,
	0x4804, 0x47dc, 0x47b3, 0x478b, 0x4763, 0x473c, 0x4714, 0x46ed, 0x46c5,
	0x469e, 0x4677, 0x4651, 0x462a, 0x4604, 0x45de, 0x45b8, 0x4592, 0x456c,
	0x4546, 0x4521, 0x44fc, 0x44d7, 0x44b2, 0x448d, 0x4468, 0x4444, 0x441f,
	0x43fb, 0x43d7, 0x43b3, 0x4390, 0x436c, 0x4349, 0x4325, 0x4302, 0x42df,
	0x42bc, 0x4299, 0x4277, 0x4254, 0x4232, 0x4210, 0x41ee, 0x41cc, 0x41aa,
	0x4189, 0x4167, 0x4146, 0x4125, 0x4104, 0x40e3, 0x40c2, 0x40a1, 0x4081,
	0x4060, 0x4040, 0x4020, 0x4000}

func init() {
	for i := range sbrLimGainsMFloat {
		sbrLimGainsM[i] = nativeaac.Fl2fxconstSGLf(sbrLimGainsMFloat[i])
	}
	for i := range sbrLimiterBPODiv4Float {
		sbrLimiterBPODiv4[i] = nativeaac.Fl2fxconstSGLf(sbrLimiterBPODiv4Float[i])
		sbrLimiterBPODiv4DBL[i] = nativeaac.Fl2fxconstDBLf(sbrLimiterBPODiv4Float[i])
	}
	for i := range sbrSmoothFilterFloat {
		sbrSmoothFilter[i] = nativeaac.Fl2fxconstSGLf(sbrSmoothFilterFloat[i])
	}
	for i := range sbrRandomPhaseFloat {
		sbrRandomPhase[i][0] = nativeaac.Fl2fxconstSGLf(sbrRandomPhaseFloat[i][0])
		if i == 301 {
			// sbr_rom.cpp:545 — the .im of entry [301] is the literal MAXVAL_SGL,
			// not a FL2FXCONST_SGL of a float; written exactly as 0x7fff.
			sbrRandomPhase[i][1] = 0x7fff
			continue
		}
		sbrRandomPhase[i][1] = nativeaac.Fl2fxconstSGLf(sbrRandomPhaseFloat[i][1])
	}
}

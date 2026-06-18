// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// 1:1 port of the SBR-encoder CONFIGURATION tier of libSBRenc/src/sbr_encoder.cpp:
// the tuning ROM (sbrenc_rom.cpp:520-897), default-config setup and the
// tuning-table lookup/adjustment that turn a (bitrate, channels, sample-rate)
// request into a concrete sbrConfiguration:
//
//   - sbrTuningTable                   (sbrenc_rom.cpp:520-897)
//   - getSbrTuningTableIndex           (sbr_encoder.cpp:222-291)
//   - FDKsbrEnc_GetDownsampledStopFreq (sbr_encoder.cpp:364-385)
//   - FDKsbrEnc_IsSbrSettingAvail      (sbr_encoder.cpp:397-427)
//   - FDKsbrEnc_AdjustSbrSettings      (sbr_encoder.cpp:438-625)
//   - FDKsbrEnc_InitializeSbrDefaults  (sbr_encoder.cpp:638-704)
//
// HE-AAC v1 only: the CODEC_AACLD (ELD) tuning rows are retained verbatim (the
// table is one ROM) but never selected — getSbrTuningTableIndex's isForThisCore
// macro requires CODEC_AAC for a non-ELD core (AOT_SBR). The useSpeechConfig /
// lcsMode / bParametricStereo / ELD branches are taken-false. fdk-aac SBR is
// FIXED-POINT.
package sbr

// codecType is the 1:1 port of enum codecType (sbr_encoder.h:116-120).
type codecType int

const (
	codecAAC   codecType = 0 // CODEC_AAC
	codecAACLD codecType = 1 // CODEC_AACLD
)

// invalidTableIdx == INVALID_TABLE_IDX (sbr_encoder.cpp:210).
const invalidTableIdx = -1

// distanceCeilValue == DISTANCE_CEIL_VALUE (sbr_encoder.cpp:221).
const distanceCeilValue = 5000000

// SBR header default constants (sbr_def.h:179-189).
const (
	sbrXposCtrlDefault        = 2 // SBR_XPOS_CTRL_DEFAULT
	sbrFreqScaleDefault       = 2 // SBR_FREQ_SCALE_DEFAULT
	sbrAlterScaleDefault      = 1 // SBR_ALTER_SCALE_DEFAULT
	sbrNoiseBandsDefault      = 2 // SBR_NOISE_BANDS_DEFAULT
	sbrLimiterBandsDefault    = 2 // SBR_LIMITER_BANDS_DEFAULT
	sbrLimiterGainsDefault    = 2 // SBR_LIMITER_GAINS_DEFAULT
	sbrLimiterGainsInfinite   = 3 // SBR_LIMITER_GAINS_INFINITE
	sbrInterpolFreqDefault    = 1 // SBR_INTERPOL_FREQ_DEFAULT
	sbrSmoothingLengthDefault = 0 // SBR_SMOOTHING_LENGTH_DEFAULT
)

// SbrTuningEntry is the 1:1 port of struct sbrTuningTable_t (sbr_encoder.h:148-166).
type SbrTuningEntry struct {
	CoreCoder       codecType
	BitrateFrom     uint // inclusive
	BitrateTo       uint // exclusive
	SampleRate      uint
	NumChannels     uint8
	StartFreq       uint8
	StartFreqSpeech uint8
	StopFreq        uint8
	StopFreqSpeech  uint8
	NumNoiseBands   uint8
	NoiseFloorOff   uint8
	NoiseMaxLevel   int8
	StereoMode      SbrStereoMode
	FreqScale       uint8
}

// sbrTuningTable is the 1:1 port of sbrTuningTable[] (sbrenc_rom.cpp:520-894).
// Column order matches the C aggregate initialiser: {coreCoder, bitrateFrom,
// bitrateTo, sampleRate, numChannels, startFreq, startFreqSpeech, stopFreq,
// stopFreqSpeech, numNoiseBands, noiseFloorOffset, noiseMaxLevel, stereoMode,
// freqScale}.
var sbrTuningTable = []SbrTuningEntry{
	// HE-AAC section — mono.
	// 8/16 kHz dual rate
	{codecAAC, 8000, 10000, 8000, 1, 7, 6, 11, 10, 1, 0, 6, SbrMono, 3},
	{codecAAC, 10000, 12000, 8000, 1, 11, 7, 13, 12, 1, 0, 6, SbrMono, 3},
	{codecAAC, 12000, 16001, 8000, 1, 14, 10, 13, 13, 1, 0, 6, SbrMono, 3},
	{codecAAC, 16000, 24000, 8000, 1, 14, 10, 14, 14, 2, 0, 3, SbrMono, 2},
	{codecAAC, 24000, 32000, 8000, 1, 14, 10, 14, 14, 2, 0, 3, SbrMono, 2},
	{codecAAC, 32000, 48001, 8000, 1, 14, 11, 15, 15, 2, 0, 3, SbrMono, 2},
	// 11/22 kHz dual rate
	{codecAAC, 8000, 10000, 11025, 1, 5, 4, 6, 6, 1, 0, 6, SbrMono, 3},
	{codecAAC, 10000, 12000, 11025, 1, 8, 5, 12, 9, 1, 0, 6, SbrMono, 3},
	{codecAAC, 12000, 16000, 11025, 1, 12, 8, 13, 8, 1, 0, 6, SbrMono, 3},
	{codecAAC, 16000, 20000, 11025, 1, 12, 8, 13, 8, 1, 0, 6, SbrMono, 3},
	{codecAAC, 20000, 24001, 11025, 1, 13, 9, 13, 8, 1, 0, 6, SbrMono, 3},
	{codecAAC, 24000, 32000, 11025, 1, 14, 10, 14, 9, 2, 0, 3, SbrMono, 2},
	{codecAAC, 32000, 48000, 11025, 1, 15, 11, 15, 10, 2, 0, 3, SbrMono, 2},
	{codecAAC, 48000, 64001, 11025, 1, 15, 11, 15, 10, 2, 0, 3, SbrMono, 1},
	// 12/24 kHz dual rate
	{codecAAC, 8000, 10000, 12000, 1, 4, 3, 6, 6, 1, 0, 6, SbrMono, 3},
	{codecAAC, 10000, 12000, 12000, 1, 7, 4, 11, 8, 1, 0, 6, SbrMono, 3},
	{codecAAC, 12000, 16000, 12000, 1, 11, 7, 12, 8, 1, 0, 6, SbrMono, 3},
	{codecAAC, 16000, 20000, 12000, 1, 11, 7, 12, 8, 1, 0, 6, SbrMono, 3},
	{codecAAC, 20000, 24001, 12000, 1, 12, 8, 12, 8, 1, 0, 6, SbrMono, 3},
	{codecAAC, 24000, 32000, 12000, 1, 13, 9, 13, 9, 2, 0, 3, SbrMono, 2},
	{codecAAC, 32000, 48000, 12000, 1, 14, 10, 14, 10, 2, 0, 3, SbrMono, 2},
	{codecAAC, 48000, 64001, 12000, 1, 14, 11, 15, 11, 2, 0, 3, SbrMono, 1},
	// 16/32 kHz dual rate
	{codecAAC, 8000, 10000, 16000, 1, 1, 1, 0, 0, 1, 0, 6, SbrMono, 3},
	{codecAAC, 10000, 12000, 16000, 1, 2, 1, 6, 0, 1, 0, 6, SbrMono, 3},
	{codecAAC, 12000, 16000, 16000, 1, 4, 2, 6, 0, 1, 0, 6, SbrMono, 3},
	{codecAAC, 16000, 18000, 16000, 1, 4, 2, 8, 3, 1, 0, 6, SbrMono, 3},
	{codecAAC, 18000, 22000, 16000, 1, 6, 5, 11, 7, 2, 0, 6, SbrMono, 2},
	{codecAAC, 22000, 28000, 16000, 1, 10, 9, 12, 8, 2, 0, 6, SbrMono, 2},
	{codecAAC, 28000, 36000, 16000, 1, 12, 12, 13, 13, 2, 0, 3, SbrMono, 2},
	{codecAAC, 36000, 44000, 16000, 1, 14, 14, 13, 13, 2, 0, 3, SbrMono, 1},
	{codecAAC, 44000, 64001, 16000, 1, 14, 14, 13, 13, 2, 0, 3, SbrMono, 1},
	// 22.05/44.1 kHz dual rate
	{codecAAC, 11369, 16000, 22050, 1, 3, 1, 4, 4, 1, 0, 6, SbrMono, 3},
	{codecAAC, 16000, 18000, 22050, 1, 3, 1, 5, 4, 1, 0, 6, SbrMono, 3},
	{codecAAC, 18000, 22000, 22050, 1, 4, 4, 8, 5, 2, 0, 6, SbrMono, 2},
	{codecAAC, 22000, 28000, 22050, 1, 7, 6, 8, 6, 2, 0, 6, SbrMono, 2},
	{codecAAC, 28000, 36000, 22050, 1, 10, 10, 9, 9, 2, 0, 3, SbrMono, 2},
	{codecAAC, 36000, 44000, 22050, 1, 11, 11, 10, 10, 2, 0, 3, SbrMono, 1},
	{codecAAC, 44000, 64001, 22050, 1, 13, 13, 12, 12, 2, 0, 3, SbrMono, 1},
	// 24/48 kHz dual rate
	{codecAAC, 12000, 16000, 24000, 1, 3, 1, 4, 4, 1, 0, 6, SbrMono, 3},
	{codecAAC, 16000, 18000, 24000, 1, 3, 1, 5, 4, 1, 0, 6, SbrMono, 3},
	{codecAAC, 18000, 22000, 24000, 1, 4, 3, 8, 5, 2, 0, 6, SbrMono, 2},
	{codecAAC, 22000, 28000, 24000, 1, 7, 6, 8, 6, 2, 0, 6, SbrMono, 2},
	{codecAAC, 28000, 36000, 24000, 1, 10, 10, 9, 9, 2, 0, 3, SbrMono, 2},
	{codecAAC, 36000, 44000, 24000, 1, 11, 11, 10, 10, 2, 0, 3, SbrMono, 1},
	{codecAAC, 44000, 64001, 24000, 1, 13, 13, 11, 11, 2, 0, 3, SbrMono, 1},
	// 32/64 kHz dual rate
	{codecAAC, 24000, 36000, 32000, 1, 4, 4, 4, 4, 2, 0, 3, SbrMono, 3},
	{codecAAC, 36000, 60000, 32000, 1, 7, 7, 6, 6, 2, 0, 3, SbrMono, 2},
	{codecAAC, 60000, 72000, 32000, 1, 9, 9, 8, 8, 2, 0, 3, SbrMono, 1},
	{codecAAC, 72000, 100000, 32000, 1, 11, 11, 10, 10, 2, 0, 3, SbrMono, 1},
	{codecAAC, 100000, 160001, 32000, 1, 13, 13, 11, 11, 2, 0, 3, SbrMono, 1},
	// 44.1/88.2 kHz dual rate
	{codecAAC, 24000, 36000, 44100, 1, 4, 4, 4, 4, 2, 0, 3, SbrMono, 3},
	{codecAAC, 36000, 60000, 44100, 1, 7, 7, 6, 6, 2, 0, 3, SbrMono, 2},
	{codecAAC, 60000, 72000, 44100, 1, 9, 9, 8, 8, 2, 0, 3, SbrMono, 1},
	{codecAAC, 72000, 100000, 44100, 1, 11, 11, 10, 10, 2, 0, 3, SbrMono, 1},
	{codecAAC, 100000, 160001, 44100, 1, 13, 13, 11, 11, 2, 0, 3, SbrMono, 1},
	// 48/96 kHz dual rate
	{codecAAC, 32000, 36000, 48000, 1, 4, 4, 9, 9, 2, 0, 3, SbrMono, 3},
	{codecAAC, 36000, 60000, 48000, 1, 7, 7, 10, 10, 2, 0, 3, SbrMono, 2},
	{codecAAC, 60000, 72000, 48000, 1, 9, 9, 10, 10, 2, 0, 3, SbrMono, 1},
	{codecAAC, 72000, 100000, 48000, 1, 11, 11, 11, 11, 2, 0, 3, SbrMono, 1},
	{codecAAC, 100000, 160001, 48000, 1, 13, 13, 11, 11, 2, 0, 3, SbrMono, 1},

	// HE-AAC section — stereo.
	// 08/16 kHz dual rate
	{codecAAC, 16000, 24000, 8000, 2, 6, 6, 9, 7, 1, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 24000, 28000, 8000, 2, 9, 9, 11, 9, 1, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 28000, 36000, 8000, 2, 11, 9, 11, 9, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 36000, 44000, 8000, 2, 13, 11, 13, 11, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 44000, 52000, 8000, 2, 14, 12, 13, 12, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 52000, 60000, 8000, 2, 14, 14, 13, 13, 3, 0, -3, SbrSwitchLrc, 1},
	{codecAAC, 60000, 76000, 8000, 2, 14, 14, 13, 13, 3, 0, -3, SbrLeftRight, 1},
	{codecAAC, 76000, 128001, 8000, 2, 14, 14, 13, 13, 3, 0, -3, SbrLeftRight, 1},
	// 11/22 kHz dual rate
	{codecAAC, 16000, 24000, 11025, 2, 7, 5, 9, 7, 1, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 24000, 28000, 11025, 2, 10, 8, 10, 8, 1, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 28000, 36000, 11025, 2, 12, 8, 12, 8, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 36000, 44000, 11025, 2, 13, 9, 13, 9, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 44000, 52000, 11025, 2, 14, 11, 13, 11, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 52000, 60000, 11025, 2, 15, 15, 13, 13, 3, 0, -3, SbrSwitchLrc, 1},
	{codecAAC, 60000, 76000, 11025, 2, 15, 15, 13, 13, 3, 0, -3, SbrLeftRight, 1},
	{codecAAC, 76000, 128001, 11025, 2, 15, 15, 13, 13, 3, 0, -3, SbrLeftRight, 1},
	// 12/24 kHz dual rate
	{codecAAC, 16000, 24000, 12000, 2, 6, 4, 9, 7, 1, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 24000, 28000, 12000, 2, 9, 7, 10, 8, 1, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 28000, 36000, 12000, 2, 11, 7, 12, 8, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 36000, 44000, 12000, 2, 12, 9, 12, 9, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 44000, 52000, 12000, 2, 13, 12, 13, 12, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 52000, 60000, 12000, 2, 14, 14, 13, 13, 3, 0, -3, SbrSwitchLrc, 1},
	{codecAAC, 60000, 76000, 12000, 2, 14, 14, 13, 13, 3, 0, -3, SbrLeftRight, 1},
	{codecAAC, 76000, 128001, 12000, 2, 14, 14, 13, 13, 3, 0, -3, SbrLeftRight, 1},
	// 16/32 kHz dual rate
	{codecAAC, 16000, 24000, 16000, 2, 4, 2, 1, 0, 1, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 24000, 28000, 16000, 2, 8, 7, 10, 8, 1, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 28000, 36000, 16000, 2, 10, 9, 12, 11, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 36000, 44000, 16000, 2, 13, 13, 13, 13, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 44000, 52000, 16000, 2, 14, 14, 13, 13, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 52000, 60000, 16000, 2, 14, 14, 13, 13, 3, 0, -3, SbrSwitchLrc, 1},
	{codecAAC, 60000, 76000, 16000, 2, 14, 14, 13, 13, 3, 0, -3, SbrLeftRight, 1},
	{codecAAC, 76000, 128001, 16000, 2, 14, 14, 13, 13, 3, 0, -3, SbrLeftRight, 1},
	// 22.05/44.1 kHz dual rate
	{codecAAC, 16000, 24000, 22050, 2, 2, 1, 1, 0, 1, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 24000, 28000, 22050, 2, 5, 4, 6, 5, 1, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 28000, 32000, 22050, 2, 5, 4, 8, 7, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 32000, 36000, 22050, 2, 7, 6, 8, 7, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 36000, 44000, 22050, 2, 10, 10, 9, 9, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 44000, 52000, 22050, 2, 12, 12, 9, 9, 3, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 52000, 60000, 22050, 2, 13, 13, 10, 10, 3, 0, -3, SbrSwitchLrc, 1},
	{codecAAC, 60000, 76000, 22050, 2, 14, 14, 12, 12, 3, 0, -3, SbrLeftRight, 1},
	{codecAAC, 76000, 128001, 22050, 2, 14, 14, 12, 12, 3, 0, -3, SbrLeftRight, 1},
	// 24/48 kHz dual rate
	{codecAAC, 16000, 24000, 24000, 2, 2, 1, 1, 0, 1, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 24000, 28000, 24000, 2, 5, 5, 6, 6, 1, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 28000, 36000, 24000, 2, 7, 6, 8, 7, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 36000, 44000, 24000, 2, 10, 10, 9, 9, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 44000, 52000, 24000, 2, 12, 12, 9, 9, 3, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 52000, 60000, 24000, 2, 13, 13, 10, 10, 3, 0, -3, SbrSwitchLrc, 1},
	{codecAAC, 60000, 76000, 24000, 2, 14, 14, 12, 12, 3, 0, -3, SbrLeftRight, 1},
	{codecAAC, 76000, 128001, 24000, 2, 14, 14, 12, 12, 3, 0, -3, SbrLeftRight, 1},
	// 32/64 kHz dual rate
	{codecAAC, 32000, 60000, 32000, 2, 4, 4, 4, 4, 2, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 60000, 80000, 32000, 2, 7, 7, 6, 6, 3, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 80000, 112000, 32000, 2, 9, 9, 8, 8, 3, 0, -3, SbrLeftRight, 1},
	{codecAAC, 112000, 144000, 32000, 2, 11, 11, 10, 10, 3, 0, -3, SbrLeftRight, 1},
	{codecAAC, 144000, 256001, 32000, 2, 13, 13, 11, 11, 3, 0, -3, SbrLeftRight, 1},
	// 44.1/88.2 kHz dual rate
	{codecAAC, 32000, 60000, 44100, 2, 4, 4, 4, 4, 2, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 60000, 80000, 44100, 2, 7, 7, 6, 6, 3, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 80000, 112000, 44100, 2, 9, 9, 8, 8, 3, 0, -3, SbrLeftRight, 1},
	{codecAAC, 112000, 144000, 44100, 2, 11, 11, 10, 10, 3, 0, -3, SbrLeftRight, 1},
	{codecAAC, 144000, 256001, 44100, 2, 13, 13, 11, 11, 3, 0, -3, SbrLeftRight, 1},
	// 48/96 kHz dual rate
	{codecAAC, 36000, 60000, 48000, 2, 4, 4, 9, 9, 2, 0, -3, SbrSwitchLrc, 3},
	{codecAAC, 60000, 80000, 48000, 2, 7, 7, 9, 9, 3, 0, -3, SbrSwitchLrc, 2},
	{codecAAC, 80000, 112000, 48000, 2, 9, 9, 10, 10, 3, 0, -3, SbrLeftRight, 1},
	{codecAAC, 112000, 144000, 48000, 2, 11, 11, 11, 11, 3, 0, -3, SbrLeftRight, 1},
	{codecAAC, 144000, 256001, 48000, 2, 13, 13, 11, 11, 3, 0, -3, SbrLeftRight, 1},

	// AAC LOW DELAY section (retained verbatim; never selected for AOT_SBR).
	{codecAACLD, 8000, 32000, 12000, 1, 1, 1, 0, 0, 1, 0, 6, SbrMono, 3},
	// mono 16/32
	{codecAACLD, 16000, 18000, 16000, 1, 4, 5, 9, 7, 1, 0, 6, SbrMono, 3},
	{codecAACLD, 18000, 22000, 16000, 1, 7, 7, 12, 12, 1, 6, 9, SbrMono, 3},
	{codecAACLD, 22000, 28000, 16000, 1, 6, 6, 9, 9, 2, 3, 6, SbrMono, 3},
	{codecAACLD, 28000, 36000, 16000, 1, 8, 8, 12, 7, 2, 9, 12, SbrMono, 3},
	{codecAACLD, 36000, 44000, 16000, 1, 10, 14, 12, 13, 2, 0, 3, SbrMono, 1},
	{codecAACLD, 44000, 64001, 16000, 1, 11, 14, 13, 13, 2, 0, 3, SbrMono, 1},
	// 22.05/44.1
	{codecAACLD, 18000, 22000, 22050, 1, 4, 4, 5, 5, 2, 0, 6, SbrMono, 3},
	{codecAACLD, 22000, 28000, 22050, 1, 5, 5, 6, 6, 2, 0, 6, SbrMono, 2},
	{codecAACLD, 28000, 36000, 22050, 1, 7, 8, 8, 8, 2, 0, 3, SbrMono, 2},
	{codecAACLD, 36000, 44000, 22050, 1, 9, 9, 9, 9, 2, 0, 3, SbrMono, 1},
	{codecAACLD, 44000, 52000, 22050, 1, 12, 11, 11, 11, 2, 0, 3, SbrMono, 1},
	{codecAACLD, 52000, 64001, 22050, 1, 13, 11, 11, 10, 2, 0, 3, SbrMono, 1},
	// 24/48
	{codecAACLD, 20000, 22000, 24000, 1, 3, 4, 8, 8, 2, 0, 6, SbrMono, 2},
	{codecAACLD, 22000, 28000, 24000, 1, 3, 8, 8, 7, 2, 0, 3, SbrMono, 2},
	{codecAACLD, 28000, 36000, 24000, 1, 4, 8, 8, 7, 2, 0, 3, SbrMono, 2},
	{codecAACLD, 36000, 56000, 24000, 1, 8, 9, 9, 8, 2, 0, 3, SbrMono, 1},
	{codecAACLD, 56000, 64001, 24000, 1, 13, 11, 11, 10, 2, 0, 3, SbrMono, 1},
	// 32/64
	{codecAACLD, 24000, 36000, 32000, 1, 4, 4, 4, 4, 2, 0, 3, SbrMono, 3},
	{codecAACLD, 36000, 60000, 32000, 1, 7, 7, 6, 6, 2, 0, 3, SbrMono, 2},
	{codecAACLD, 60000, 72000, 32000, 1, 9, 9, 8, 8, 2, 0, 3, SbrMono, 1},
	{codecAACLD, 72000, 100000, 32000, 1, 11, 11, 10, 10, 2, 0, 3, SbrMono, 1},
	{codecAACLD, 100000, 160001, 32000, 1, 13, 13, 11, 11, 2, 0, 3, SbrMono, 1},
	// 44/88
	{codecAACLD, 36000, 60000, 44100, 1, 8, 7, 6, 9, 2, 0, 3, SbrMono, 2},
	{codecAACLD, 60000, 72000, 44100, 1, 9, 9, 10, 10, 2, 0, 3, SbrMono, 1},
	{codecAACLD, 72000, 100000, 44100, 1, 11, 11, 11, 11, 2, 0, 3, SbrMono, 1},
	{codecAACLD, 100000, 160001, 44100, 1, 13, 13, 11, 11, 2, 0, 3, SbrMono, 1},
	// 48/96
	{codecAACLD, 36000, 60000, 48000, 1, 4, 7, 4, 4, 2, 0, 3, SbrMono, 3},
	{codecAACLD, 60000, 72000, 48000, 1, 9, 9, 10, 10, 2, 0, 3, SbrMono, 1},
	{codecAACLD, 72000, 100000, 48000, 1, 11, 11, 11, 11, 2, 0, 3, SbrMono, 1},
	{codecAACLD, 100000, 160001, 48000, 1, 13, 13, 11, 11, 2, 0, 3, SbrMono, 1},
	// LD stereo 16/32
	{codecAACLD, 32000, 36000, 16000, 2, 10, 9, 12, 11, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAACLD, 36000, 44000, 16000, 2, 13, 13, 13, 13, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAACLD, 44000, 52000, 16000, 2, 10, 9, 11, 9, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAACLD, 52000, 60000, 16000, 2, 14, 14, 13, 13, 3, 0, -3, SbrSwitchLrc, 1},
	{codecAACLD, 60000, 76000, 16000, 2, 14, 14, 13, 13, 3, 0, -3, SbrLeftRight, 1},
	{codecAACLD, 76000, 128001, 16000, 2, 14, 14, 13, 13, 3, 0, -3, SbrLeftRight, 1},
	// 22.05/44.1
	{codecAACLD, 32000, 36000, 22050, 2, 5, 4, 7, 6, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAACLD, 36000, 44000, 22050, 2, 5, 8, 8, 8, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAACLD, 44000, 52000, 22050, 2, 7, 10, 8, 8, 3, 0, -3, SbrSwitchLrc, 2},
	{codecAACLD, 52000, 60000, 22050, 2, 9, 11, 9, 9, 3, 0, -3, SbrSwitchLrc, 1},
	{codecAACLD, 60000, 76000, 22050, 2, 10, 12, 10, 11, 3, 0, -3, SbrLeftRight, 1},
	{codecAACLD, 76000, 82000, 22050, 2, 12, 12, 11, 11, 3, 0, -3, SbrLeftRight, 1},
	{codecAACLD, 82000, 128001, 22050, 2, 13, 12, 11, 11, 3, 0, -3, SbrLeftRight, 1},
	// 24/48
	{codecAACLD, 32000, 36000, 24000, 2, 5, 4, 7, 6, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAACLD, 36000, 44000, 24000, 2, 4, 8, 8, 8, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAACLD, 44000, 52000, 24000, 2, 6, 10, 8, 8, 3, 0, -3, SbrSwitchLrc, 2},
	{codecAACLD, 52000, 60000, 24000, 2, 9, 11, 9, 9, 3, 0, -3, SbrSwitchLrc, 1},
	{codecAACLD, 60000, 76000, 24000, 2, 11, 12, 10, 11, 3, 0, -3, SbrLeftRight, 1},
	{codecAACLD, 76000, 88000, 24000, 2, 12, 13, 11, 11, 3, 0, -3, SbrLeftRight, 1},
	{codecAACLD, 88000, 128001, 24000, 2, 13, 13, 11, 11, 3, 0, -3, SbrLeftRight, 1},
	// 32/64
	{codecAACLD, 60000, 80000, 32000, 2, 7, 7, 6, 6, 3, 0, -3, SbrSwitchLrc, 2},
	{codecAACLD, 80000, 112000, 32000, 2, 9, 9, 8, 8, 3, 0, -3, SbrLeftRight, 1},
	{codecAACLD, 112000, 144000, 32000, 2, 11, 11, 10, 10, 3, 0, -3, SbrLeftRight, 1},
	{codecAACLD, 144000, 256001, 32000, 2, 13, 13, 11, 11, 3, 0, -3, SbrLeftRight, 1},
	// 44.1/88.2
	{codecAACLD, 60000, 80000, 44100, 2, 7, 7, 6, 6, 3, 0, -3, SbrSwitchLrc, 2},
	{codecAACLD, 80000, 112000, 44100, 2, 10, 10, 8, 8, 3, 0, -3, SbrLeftRight, 1},
	{codecAACLD, 112000, 144000, 44100, 2, 12, 12, 10, 10, 3, 0, -3, SbrLeftRight, 1},
	{codecAACLD, 144000, 256001, 44100, 2, 13, 13, 11, 11, 3, 0, -3, SbrLeftRight, 1},
	// 48/96
	{codecAACLD, 60000, 80000, 48000, 2, 7, 7, 10, 10, 2, 0, -3, SbrSwitchLrc, 2},
	{codecAACLD, 80000, 112000, 48000, 2, 9, 9, 10, 10, 3, 0, -3, SbrLeftRight, 1},
	{codecAACLD, 112000, 144000, 48000, 2, 11, 11, 11, 11, 3, 0, -3, SbrLeftRight, 1},
	{codecAACLD, 144000, 176000, 48000, 2, 12, 12, 11, 11, 3, 0, -3, SbrLeftRight, 1},
	{codecAACLD, 176000, 256001, 48000, 2, 13, 13, 11, 11, 3, 0, -3, SbrLeftRight, 1},
}

// SbrConfiguration is the 1:1 port of struct sbrConfiguration (sbr_encoder.h:168-242):
// the per-element SBR configuration produced by initializeSbrDefaults +
// adjustSbrSettings and consumed by EnvInit / initEnvChannel. HE-AAC v1 only;
// the PS/ELD-only members are retained but stay at their default values.
type SbrConfiguration struct {
	// CODEC_PARAM codecSettings.
	BitRate         int
	NChannels       int
	SampleFreq      int
	TransFac        int
	StandardBitrate int

	SendHeaderDataTime int
	UseWaveCoding      int
	CrcSbr             int
	DynBwSupported     int
	ParametricCoding   int
	DownSampleFactor   int
	FreqResFixfix      [2]FreqRes
	FResTransIsLow     uint8

	TranThr          int
	NoiseFloorOffset int
	UseSpeechConfig  uint

	SbrFrameSize      int
	SbrDataExtra      int
	AmpRes            int
	AnaMaxLevel       int
	TranFc            int
	TranDetMode       int
	Spread            int
	Stat              int
	E                 int
	StereoMode        SbrStereoMode
	DeltaTAcross      int
	DFEdge1stEnv      int32
	DFEdgeIncr        int32
	SbrInvfMode       int
	SbrXposMode       int
	SbrXposCtrl       int
	SbrXposLevel      int
	StartFreq         int
	StopFreq          int
	UseSaPan          int
	DynBwEnabled      int
	BParametricStereo int

	FreqScale     uint8
	AlterScale    int
	SbrNoiseBands int

	SbrLimiterBands    int
	SbrLimiterGains    int
	SbrInterpolFreq    int
	SbrSmoothingLength int
	InitAmpResFF       uint8
	ThresholdAmpResFFm int32
	ThresholdAmpResFFe int8
}

// getSbrTuningTableIndex is the 1:1 port of getSbrTuningTableIndex
// (sbr_encoder.cpp:222-291). core != AOT_ER_AAC_ELD (i.e. AOT_SBR) selects
// CODEC_AAC rows. pBitRateClosest is unused on the encode path (always nil).
func getSbrTuningTableIndex(bitrate, numChannels, sampleRate uint, isELD bool) int {
	bitRateClosestUpperIndex := -1
	bitRateClosestUpper := uint(0)

	isForThisCore := func(i int) bool {
		return (sbrTuningTable[i].CoreCoder == codecAACLD && isELD) ||
			(sbrTuningTable[i].CoreCoder == codecAAC && !isELD)
	}

	for i := 0; i < len(sbrTuningTable); i++ {
		if isForThisCore(i) {
			if numChannels == uint(sbrTuningTable[i].NumChannels) &&
				sampleRate == sbrTuningTable[i].SampleRate {
				if bitrate >= sbrTuningTable[i].BitrateFrom && bitrate < sbrTuningTable[i].BitrateTo {
					return i
				}
				if sbrTuningTable[i].BitrateTo <= bitrate {
					if sbrTuningTable[i].BitrateTo > bitRateClosestUpper {
						bitRateClosestUpper = sbrTuningTable[i].BitrateTo - 1
						bitRateClosestUpperIndex = i
					}
				}
			}
		}
	}

	if bitRateClosestUpperIndex >= 0 {
		return bitRateClosestUpperIndex
	}
	return invalidTableIdx
}

// getDownsampledStopFreq is the 1:1 port of FDKsbrEnc_GetDownsampledStopFreq
// (sbr_encoder.cpp:364-385).
func getDownsampledStopFreq(sampleRateCore, startFreq, stopFreq, downSampleFactor int) int {
	maxStopFreqRaw := sampleRateCore / 2

	for stopFreq > 0 && GetSbrStopFreqRAW(stopFreq, sampleRateCore) > maxStopFreqRaw {
		stopFreq--
	}
	if GetSbrStopFreqRAW(stopFreq, sampleRateCore) > maxStopFreqRaw {
		return -1
	}

	_, _, err := FindStartAndStopBand(sampleRateCore<<(downSampleFactor-1), sampleRateCore,
		32<<(downSampleFactor-1), startFreq, stopFreq)
	if err != 0 {
		return -1
	}
	return stopFreq
}

// isSbrSettingAvail is the 1:1 port of FDKsbrEnc_IsSbrSettingAvail
// (sbr_encoder.cpp:397-427). vbrMode==0 (CBR) on the integration path.
func isSbrSettingAvail(bitrate, vbrMode, numOutputChannels, sampleRateInput, sampleRateCore uint, isELD bool) bool {
	if sampleRateInput < 16000 {
		return false
	}
	if bitrate == 0 {
		switch {
		case vbrMode < 30:
			bitrate = 24000
		case vbrMode < 40:
			bitrate = 28000
		case vbrMode < 60:
			bitrate = 32000
		case vbrMode < 75:
			bitrate = 40000
		default:
			bitrate = 48000
		}
		bitrate *= numOutputChannels
	}
	return getSbrTuningTableIndex(bitrate, numOutputChannels, sampleRateCore, isELD) != invalidTableIdx
}

// adjustSbrSettings is the 1:1 port of FDKsbrEnc_AdjustSbrSettings
// (sbr_encoder.cpp:438-625). HE-AAC v1: core == AOT_AAC_LC, useSpeechConfig ==
// lcsMode == bParametricStereo == 0 (taken-false branches noted). Returns false
// on no matching tuning entry.
func adjustSbrSettings(config *SbrConfiguration, bitRate, numChannels, sampleRateCore,
	sampleRateSbr, transFac, standardBitrate, vbrMode uint, isELD bool) bool {

	config.BitRate = int(bitRate)
	config.NChannels = int(numChannels)
	config.SampleFreq = int(sampleRateCore)
	config.TransFac = int(transFac)
	config.StandardBitrate = int(standardBitrate)

	switch {
	case bitRate < 28000:
		config.ThresholdAmpResFFm = encMaxvalDBL
		config.ThresholdAmpResFFe = 7
	case bitRate >= 28000 && bitRate <= 48000:
		// FL2FXCONST_DBL(75.0f * 0.524288f / (2.0f/3.0f) / 128.0f).
		config.ThresholdAmpResFFm = fl2f(75.0 * 0.524288 / (2.0 / 3.0) / 128.0)
		config.ThresholdAmpResFFe = 7
	case bitRate > 48000:
		config.ThresholdAmpResFFm = fl2f(0)
		config.ThresholdAmpResFFe = 0
	}

	if bitRate == 0 {
		switch {
		case vbrMode < 30:
			bitRate = 24000
		case vbrMode < 40:
			bitRate = 28000
		case vbrMode < 60:
			bitRate = 32000
		case vbrMode < 75:
			bitRate = 40000
		default:
			bitRate = 48000
		}
		bitRate *= numChannels
		if numChannels == 1 {
			if sampleRateSbr == 44100 || sampleRateSbr == 48000 {
				if vbrMode < 40 {
					bitRate = 32000
				}
			}
		}
	}

	idx := getSbrTuningTableIndex(bitRate, numChannels, sampleRateCore, isELD)
	if idx == invalidTableIdx {
		return false
	}

	te := &sbrTuningTable[idx]
	config.StartFreq = int(te.StartFreq)
	config.StopFreq = int(te.StopFreq)
	// useSpeechConfig == 0: the *Speech branches are taken-false.

	// Adapt stop frequency in case of downsampled SBR (downSampleFactor == 1).
	if config.DownSampleFactor == 1 {
		dsStopFreq := getDownsampledStopFreq(int(sampleRateCore), config.StartFreq,
			config.StopFreq, config.DownSampleFactor)
		if dsStopFreq < 0 {
			return false
		}
		config.StopFreq = dsStopFreq
	}

	config.SbrNoiseBands = int(te.NumNoiseBands)
	// core == AOT_AAC_LC: the ELD init_amp_res_FF override is taken-false.
	config.NoiseFloorOffset = int(te.NoiseFloorOff)
	config.AnaMaxLevel = int(te.NoiseMaxLevel)
	config.StereoMode = te.StereoMode
	config.FreqScale = te.FreqScale

	if numChannels == 1 {
		// AOT_AAC_LC mono. useSpeechConfig==0 => 20000U threshold.
		if bitRate <= 20000 {
			config.FreqResFixfix[0] = FreqResLow
			config.FreqResFixfix[1] = FreqResLow
		}
	} else {
		// AOT_AAC_LC stereo.
		if bitRate <= 28000 {
			config.FreqResFixfix[0] = FreqResLow
			config.FreqResFixfix[1] = FreqResLow
		}
		// Additional restriction (sbr_encoder.cpp:588-594) — identical clause.
		if bitRate <= 28000 {
			config.FreqResFixfix[0] = FreqResLow
			config.FreqResFixfix[1] = FreqResLow
		}
	}

	// useSpeechConfig == 0: parametricCoding stays as defaulted.
	// core == AOT_AAC_LC: ELD init_amp_res_FF / SendHeaderDataTime branch skipped.

	if numChannels == 1 {
		if bitRate < 16000 {
			config.ParametricCoding = 0
		}
	} else {
		if bitRate < 20000 {
			config.ParametricCoding = 0
		}
	}

	config.UseSpeechConfig = 0
	config.BParametricStereo = 0
	return true
}

// initializeSbrDefaults is the 1:1 port of FDKsbrEnc_InitializeSbrDefaults
// (sbr_encoder.cpp:638-704). isLowDelay==false for HE-AAC v1. Returns false on
// an illegal frame/downsample combination.
func initializeSbrDefaults(config *SbrConfiguration, downSampleFactor int, codecGranuleLen int, isLowDelay bool) bool {
	if (downSampleFactor < 1 || downSampleFactor > 2) || (codecGranuleLen*downSampleFactor > 64*32) {
		return false
	}

	config.SendHeaderDataTime = 1000
	config.UseWaveCoding = 0
	config.CrcSbr = 0
	config.DynBwSupported = 1
	if isLowDelay {
		config.TranThr = 6000
	} else {
		config.TranThr = 13000
	}

	config.ParametricCoding = 1

	config.SbrFrameSize = codecGranuleLen * downSampleFactor
	config.DownSampleFactor = downSampleFactor

	config.SbrDataExtra = 0
	config.AmpRes = int(SbrAmpRes30)
	config.TranFc = 0
	config.TranDetMode = 1
	config.Spread = 1
	config.Stat = 0
	config.E = 1
	config.DeltaTAcross = 1
	config.DFEdge1stEnv = fl2f(0.3)
	config.DFEdgeIncr = fl2f(0.3)

	config.SbrInvfMode = int(InvfSwitched)
	config.SbrXposMode = int(XposLc)
	config.SbrXposCtrl = sbrXposCtrlDefault
	config.SbrXposLevel = 0
	config.UseSaPan = 0
	config.DynBwEnabled = 0

	config.StereoMode = SbrSwitchLrc
	config.AnaMaxLevel = 6
	config.NoiseFloorOffset = 0
	config.StartFreq = 5
	config.StopFreq = 9
	config.FreqResFixfix[0] = FreqResHigh
	config.FreqResFixfix[1] = FreqResHigh
	config.FResTransIsLow = 0

	config.FreqScale = sbrFreqScaleDefault
	config.AlterScale = sbrAlterScaleDefault
	config.SbrNoiseBands = sbrNoiseBandsDefault

	config.SbrLimiterBands = sbrLimiterBandsDefault
	config.SbrLimiterGains = sbrLimiterGainsDefault
	config.SbrInterpolFreq = sbrInterpolFreqDefault
	config.SbrSmoothingLength = sbrSmoothingLengthDefault

	return true
}

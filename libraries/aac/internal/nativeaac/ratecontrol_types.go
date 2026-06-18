// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// This file is part of the pure-Go 1:1 port of the Fraunhofer FDK-AAC
// encoder rate-control / bit-reservoir loop. It carries the `aacfdk` build
// tag so a default `go build` links none of the FDK-AAC-derived code; see
// libfdk/COPYING for the (non-FOSS-but-permissive) Fraunhofer FDK-AAC
// license. The kernels here are pure integer arithmetic, so they are
// bit-identical regardless of vectorization and need no aac_strict split.
//
// The types below are the supporting enumerations and tables the rate loop
// reads. They are faithful translations of their C counterparts; the field
// order and numeric values match the vendored headers exactly so the parity
// oracle compares like-for-like.

package nativeaac

// BitrateMode mirrors AACENC_BITRATE_MODE (aacenc.h:190). It selects constant
// vs. variable bitrate operation and indexes the VBR configuration table.
type BitrateMode int

// BitrateMode values mirror the AACENC_BR_MODE_* enumerators (aacenc.h:191).
const (
	BitrateModeInvalid BitrateMode = -1 // AACENC_BR_MODE_INVALID
	BitrateModeCBR     BitrateMode = 0  // AACENC_BR_MODE_CBR
	BitrateModeVBR1    BitrateMode = 1  // AACENC_BR_MODE_VBR_1
	BitrateModeVBR2    BitrateMode = 2  // AACENC_BR_MODE_VBR_2
	BitrateModeVBR3    BitrateMode = 3  // AACENC_BR_MODE_VBR_3
	BitrateModeVBR4    BitrateMode = 4  // AACENC_BR_MODE_VBR_4
	BitrateModeVBR5    BitrateMode = 5  // AACENC_BR_MODE_VBR_5
	BitrateModeFF      BitrateMode = 6  // AACENC_BR_MODE_FF
	BitrateModeSFR     BitrateMode = 7  // AACENC_BR_MODE_SFR
)

// isVBR mirrors the AACENC_BR_MODE_IS_VBR(brMode) macro
// (aacenc.h:203): true for the five variable-bitrate modes.
//
//	#define AACENC_BR_MODE_IS_VBR(brMode) ((brMode >= 1) && (brMode <= 5))
func (m BitrateMode) isVBR() bool { return m >= 1 && m <= 5 }

// minBufsizePerEffChan mirrors MIN_BUFSIZE_PER_EFF_CHAN (aacenc.h:113): the
// per-effective-channel input buffer size in bits, used as the bitreservoir /
// peak-bitrate ceiling.
const minBufsizePerEffChan = 6144

// ChannelMode mirrors CHANNEL_MODE (FDK_audio.h:234). It identifies the
// speaker layout; the rate loop only consults it via the configuration table
// and the mono/stereo classifier.
type ChannelMode int

// ChannelMode values mirror the MODE_* enumerators (FDK_audio.h:235).
const (
	ChannelModeInvalid      ChannelMode = -1  // MODE_INVALID
	ChannelModeUnknown      ChannelMode = 0   // MODE_UNKNOWN
	ChannelMode1            ChannelMode = 1   // MODE_1                 (C)
	ChannelMode2            ChannelMode = 2   // MODE_2                 (L+R)
	ChannelMode1_2          ChannelMode = 3   // MODE_1_2               (C, L+R)
	ChannelMode1_2_1        ChannelMode = 4   // MODE_1_2_1             (C, L+R, Rear)
	ChannelMode1_2_2        ChannelMode = 5   // MODE_1_2_2             (C, L+R, LS+RS)
	ChannelMode1_2_2_1      ChannelMode = 6   // MODE_1_2_2_1           (C, L+R, LS+RS, LFE)
	ChannelMode1_2_2_2_1    ChannelMode = 7   // MODE_1_2_2_2_1         (C, LC+RC, L+R, LS+RS, LFE)
	ChannelMode6_1          ChannelMode = 11  // MODE_6_1
	ChannelMode7_1Back      ChannelMode = 12  // MODE_7_1_BACK
	ChannelMode7_1TopFront  ChannelMode = 14  // MODE_7_1_TOP_FRONT
	ChannelMode7_1RearSurr  ChannelMode = 33  // MODE_7_1_REAR_SURROUND
	ChannelMode7_1FrontCent ChannelMode = 34  // MODE_7_1_FRONT_CENTER
	ChannelMode212          ChannelMode = 128 // MODE_212
)

// ElementMode mirrors ELEMENT_MODE (channel_map.h:118): whether the element is
// mono, stereo, or invalid.
type ElementMode int

// ElementMode values mirror the EL_MODE_* enumerators (channel_map.h:118).
const (
	ElementModeInvalid ElementMode = 0 // EL_MODE_INVALID
	ElementModeMono    ElementMode = 1 // EL_MODE_MONO
	ElementModeStereo  ElementMode = 2 // EL_MODE_STEREO
)

// AudioObjectType mirrors AUDIO_OBJECT_TYPE (FDK_audio.h) for the rate-loop's
// purposes: only the low-delay object types are consulted here.
type AudioObjectType int

// AudioObjectType values mirror the AOT_* enumerators (FDK_audio.h) used by the
// rate loop.
const (
	AOTErAACLD  AudioObjectType = 23 // AOT_ER_AAC_LD
	AOTErAACELD AudioObjectType = 39 // AOT_ER_AAC_ELD
)

// isLowDelay mirrors isLowDelay() (interface.h:164): true for the low-delay
// and enhanced-low-delay AAC object types.
//
//	inline int isLowDelay(AUDIO_OBJECT_TYPE aot) {
//	  return (aot == AOT_ER_AAC_LD || aot == AOT_ER_AAC_ELD);
//	}
func isLowDelay(aot AudioObjectType) bool {
	return aot == AOTErAACLD || aot == AOTErAACELD
}

// channelModeConfigTabEntry mirrors CHANNEL_MODE_CONFIG_TAB (channel_map.h:110).
type channelModeConfigTabEntry struct {
	encMode      ChannelMode
	nChannels    int
	nChannelsEff int
	nElements    int
}

// channelModeConfig mirrors the static channelModeConfig[] table
// (channel_map.cpp:139). Fields are {encMode, nChannels, nChannelsEff,
// nElements} in declaration order.
var channelModeConfig = []channelModeConfigTabEntry{
	{ChannelMode1, 1, 1, 1},            // chCfg  1, SCE
	{ChannelMode2, 2, 2, 1},            // chCfg  2, CPE
	{ChannelMode1_2, 3, 3, 2},          // chCfg  3, SCE,CPE
	{ChannelMode1_2_1, 4, 4, 3},        // chCfg  4, SCE,CPE,SCE
	{ChannelMode1_2_2, 5, 5, 3},        // chCfg  5, SCE,CPE,CPE
	{ChannelMode1_2_2_1, 6, 5, 4},      // chCfg  6, SCE,CPE,CPE,LFE
	{ChannelMode1_2_2_2_1, 8, 7, 5},    // chCfg  7, SCE,CPE,CPE,CPE,LFE
	{ChannelMode6_1, 7, 6, 5},          // chCfg 11, SCE,CPE,CPE,SCE,LFE
	{ChannelMode7_1Back, 8, 7, 5},      // chCfg 12, SCE,CPE,CPE,CPE,LFE
	{ChannelMode7_1TopFront, 8, 7, 5},  // chCfg 14, SCE,CPE,CPE,LFE,CPE
	{ChannelMode7_1RearSurr, 8, 7, 5},  // same as MODE_7_1_BACK
	{ChannelMode7_1FrontCent, 8, 7, 5}, // same as MODE_1_2_2_2_1
}

// getChannelModeConfiguration mirrors FDKaacEnc_GetChannelModeConfiguration()
// (channel_map.cpp:649): return the configuration table entry matching mode, or
// nil (C: NULL) when the mode is not present in the table.
func getChannelModeConfiguration(mode ChannelMode) *channelModeConfigTabEntry {
	for i := range channelModeConfig {
		if channelModeConfig[i].encMode == mode {
			return &channelModeConfig[i]
		}
	}
	return nil
}

// getMonoStereoMode mirrors FDKaacEnc_GetMonoStereoMode()
// (channel_map.cpp:619): classify a channel mode as mono, stereo, or invalid.
func getMonoStereoMode(mode ChannelMode) ElementMode {
	switch mode {
	case ChannelMode1: // mono setups
		return ElementModeMono
	case ChannelMode2, // stereo setups
		ChannelMode1_2,
		ChannelMode1_2_1,
		ChannelMode1_2_2,
		ChannelMode1_2_2_1,
		ChannelMode6_1,
		ChannelMode1_2_2_2_1,
		ChannelMode7_1RearSurr,
		ChannelMode7_1FrontCent,
		ChannelMode7_1Back,
		ChannelMode7_1TopFront:
		return ElementModeStereo
	default: // error
		return ElementModeInvalid
	}
}

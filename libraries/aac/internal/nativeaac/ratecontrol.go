// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// This file is the pure-Go 1:1 port of the Fraunhofer FDK-AAC encoder
// rate-control / bit-reservoir loop, translated from libAACenc/src/aacenc.cpp
// (and the VBR table it shares with aacenc_lib.cpp). It carries the `aacfdk`
// build tag so a default `go build` links none of the FDK-AAC-derived code;
// see libfdk/COPYING for the (non-FOSS-but-permissive) Fraunhofer FDK-AAC
// license. The default `!aacfdk` build provides no rate loop and the public
// libraries/aac surface returns ErrEngineRequiresFDK.
//
// Every function here is pure integer arithmetic, so it is bit-identical
// regardless of vectorization and needs no aac_strict FP split. The
// translation is faithful: control flow, the truncating integer divisions,
// and the saturating fMin/fMax clamps match the C exactly — do not "improve"
// the algorithm.
//
// The `bitrateMode` / `aot` clamping path in FDKaacEnc_EncodeFrame, the
// per-frame two-loop scalefactor search (qc_main.cpp) and the SBR/PS/MPS
// dependent helpers FDKaacEnc_GetCBRBitrate / aacEncoder_LimitBitrate
// (aacenc_lib.cpp) are deliberately NOT in this slice: they reach into the
// quantizer, SBR and transport subsystems that are not yet ported. This slice
// covers the self-contained integer rate-loop primitives of aacenc.cpp.

package nativeaac

// fMaxI mirrors fMax(INT,INT) (common_fix.h:407): the larger of two ints.
func fMaxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// fMinI mirrors fMin(INT,INT) (common_fix.h:408): the smaller of two ints.
func fMinI(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CalcBitsPerFrame mirrors FDKaacEnc_CalcBitsPerFrame() (aacenc.cpp:124):
// convert a bitrate (bits/s) to the average number of payload bits in one
// frame. The shift loop divides out the common power-of-two factor of
// frameLength and samplingRate before the multiply so the intermediate
// product cannot overflow; the division then truncates toward zero exactly as
// the C integer division does.
func CalcBitsPerFrame(bitRate, frameLength, samplingRate int) int {
	shift := 0
	for (frameLength&^((1<<(shift+1))-1)) == frameLength &&
		(samplingRate&^((1<<(shift+1))-1)) == samplingRate {
		shift++
	}

	return (bitRate * (frameLength >> shift)) / (samplingRate >> shift)
}

// CalcBitrate mirrors FDKaacEnc_CalcBitrate() (aacenc.cpp:135): the inverse of
// CalcBitsPerFrame, converting a per-frame payload-bit budget back to a
// bitrate (bits/s). It uses the identical power-of-two reduction.
func CalcBitrate(bitsPerFrame, frameLength, samplingRate int) int {
	shift := 0
	for (frameLength&^((1<<(shift+1))-1)) == frameLength &&
		(samplingRate&^((1<<(shift+1))-1)) == samplingRate {
		shift++
	}

	return (bitsPerFrame * (samplingRate >> shift)) / (frameLength >> shift)
}

// StaticBitsProvider models the transportEnc_GetStaticBits(hTpEnc,
// averageBitsPerFrame) indirect call in FDKaacEnc_LimitBitrate
// (aacenc.cpp:172). In the C code hTpEnc may be NULL, in which case a worst
// case of 208 transport bits is assumed; passing a nil StaticBitsProvider here
// reproduces that NULL branch exactly. A non-nil provider returns the number
// of static transport bits for the given average per-frame budget.
type StaticBitsProvider func(averageBitsPerFrame int) int

// LimitBitrate mirrors FDKaacEnc_LimitBitrate() (aacenc.cpp:150): iteratively
// clamp bitRate into the band the encoder can actually realise given the
// minimum per-frame payload, the transport overhead and the input-buffer
// ceiling. When pAverageBitsPerFrame is non-nil it receives the average
// per-frame payload of the *final* iteration, matching the C out-parameter
// (which is written every iteration; the last write survives). hTpEnc is the
// optional static-bits provider (nil reproduces the C NULL / 208-bit branch).
//
// The loop runs while bitRate keeps changing, capped at four passes
// (`iter++ < 3`), identical to the C do/while.
func LimitBitrate(
	hTpEnc StaticBitsProvider,
	aot AudioObjectType,
	coreSamplingRate, frameLength, nChannels, nChannelsEff, bitRate, averageBits int,
	pAverageBitsPerFrame *int,
	bitrateMode BitrateMode,
	nSubFrames int,
) int {
	_ = averageBits // present in the C signature; unused by the loop body.

	var transportBits, prevBitRate, averageBitsPerFrame int
	minBitrate := 0
	iter := 0
	minBitsPerFrame := 40 * nChannels
	if isLowDelay(aot) {
		minBitrate = 8000 * nChannelsEff
	}

	for {
		prevBitRate = bitRate
		averageBitsPerFrame =
			CalcBitsPerFrame(bitRate, frameLength, coreSamplingRate) / nSubFrames

		if pAverageBitsPerFrame != nil {
			*pAverageBitsPerFrame = averageBitsPerFrame
		}

		if hTpEnc != nil {
			transportBits = hTpEnc(averageBitsPerFrame)
		} else {
			// Assume some worst case.
			transportBits = 208
		}

		bitRate = fMaxI(bitRate,
			fMaxI(minBitrate,
				CalcBitrate((minBitsPerFrame+transportBits),
					frameLength, coreSamplingRate)))
		// FDK_ASSERT(bitRate >= 0)

		bitRate = fMinI(bitRate, CalcBitrate(
			(nChannelsEff*minBufsizePerEffChan),
			frameLength, coreSamplingRate))
		// FDK_ASSERT(bitRate >= 0)

		if prevBitRate == bitRate || iter >= 3 {
			break
		}
		iter++
	}

	return bitRate
}

// configTabEntryVBR mirrors the anonymous CONFIG_TAB_ENTRY_VBR struct
// (aacenc.cpp:194): a VBR mode and its {mono, stereo} per-channel bitrates.
type configTabEntryVBR struct {
	bitrateMode BitrateMode
	chanBitrate [2]int // [0]=mono, [1]=stereo
}

// configTabVBR mirrors the static configTabVBR[] table (aacenc.cpp:199).
var configTabVBR = []configTabEntryVBR{
	{BitrateModeCBR, [2]int{0, 0}},
	{BitrateModeVBR1, [2]int{32000, 20000}},
	{BitrateModeVBR2, [2]int{40000, 32000}},
	{BitrateModeVBR3, [2]int{56000, 48000}},
	{BitrateModeVBR4, [2]int{72000, 64000}},
	{BitrateModeVBR5, [2]int{112000, 96000}},
}

// GetVBRBitrate mirrors FDKaacEnc_GetVBRBitrate() (aacenc.cpp:216): look up the
// overall target bitrate for a VBR quality mode and channel mode by indexing
// configTabVBR with the mono/stereo column and scaling by the effective
// channel count. Non-VBR modes return 0.
func GetVBRBitrate(bitrateMode BitrateMode, channelMode ChannelMode) int {
	bitrate := 0
	monoStereoMode := 0 // default mono

	if getMonoStereoMode(channelMode) == ElementModeStereo {
		monoStereoMode = 1
	}

	switch bitrateMode {
	case BitrateModeVBR1,
		BitrateModeVBR2,
		BitrateModeVBR3,
		BitrateModeVBR4,
		BitrateModeVBR5:
		bitrate = configTabVBR[bitrateMode].chanBitrate[monoStereoMode]
	case BitrateModeInvalid,
		BitrateModeCBR,
		BitrateModeSFR,
		BitrateModeFF:
		bitrate = 0
	default:
		bitrate = 0
	}

	// convert channel bitrate to overall bitrate
	bitrate *= getChannelModeConfiguration(channelMode).nChannelsEff

	return bitrate
}

// AdjustVBRBitrateMode mirrors FDKaacEnc_AdjustVBRBitrateMode()
// (aacenc.cpp:258): pick the VBR mode whose per-channel target does not exceed
// the supplied bitrate, scanning the table from the top down. A bitrate of -1
// leaves the mode unchanged. The result is forced to AACENC_BR_MODE_INVALID
// unless it is one of the five VBR modes.
func AdjustVBRBitrateMode(bitrateMode BitrateMode, bitrate int, channelMode ChannelMode) BitrateMode {
	newBitrateMode := bitrateMode

	if bitrate != -1 {
		monoStereoMode := 0
		if getMonoStereoMode(channelMode) == ElementModeStereo {
			monoStereoMode = 1
		}
		nChannelsEff := getChannelModeConfiguration(channelMode).nChannelsEff
		newBitrateMode = BitrateModeInvalid

		for idx := len(configTabVBR) - 1; idx >= 0; idx-- {
			if bitrate >= configTabVBR[idx].chanBitrate[monoStereoMode]*nChannelsEff {
				if configTabVBR[idx].chanBitrate[monoStereoMode]*nChannelsEff <
					GetVBRBitrate(bitrateMode, channelMode) {
					newBitrateMode = configTabVBR[idx].bitrateMode
				} else {
					newBitrateMode = bitrateMode
				}
				break
			}
		}
	}

	if newBitrateMode.isVBR() {
		return newBitrateMode
	}
	return BitrateModeInvalid
}

const fdkIntMax = 0x7fffffff // FDK_INT_MAX (32-bit INT maximum)

// EncBitresToTpBitres mirrors FDKaacEnc_EncBitresToTpBitres()
// (aacenc.cpp:295): translate the encoder's internal bitreservoir level into
// the value the transport library expects. The C reads hAacEnc->bitrateMode,
// hAacEnc->qcKernel->bitResTot, hAacEnc->channelMapping.nChannelsEff and
// hAacEnc->config->audioMuxVersion; those handle fields are passed in here
// (bitResTot, nChannelsEff, audioMuxVersion) because the surrounding AAC_ENC
// aggregate is not part of this slice. Behaviour is identical: CBR forwards
// the encoder reservoir, VBR signals FDK_INT_MAX, SFR/INVALID signal 0, and
// audioMuxVersion==2 overrides everything with the per-channel input-buffer
// size.
func EncBitresToTpBitres(bitrateMode BitrateMode, bitResTot, nChannelsEff, audioMuxVersion int) int {
	transportBitreservoir := 0

	switch bitrateMode {
	case BitrateModeCBR:
		transportBitreservoir = bitResTot // encoder bitreservoir level
	case BitrateModeVBR1,
		BitrateModeVBR2,
		BitrateModeVBR3,
		BitrateModeVBR4,
		BitrateModeVBR5:
		transportBitreservoir = fdkIntMax // signal variable bitrate
	case BitrateModeSFR:
		transportBitreservoir = 0 // super framing and fixed framing
		// without bitreservoir signaling
	case BitrateModeInvalid:
		transportBitreservoir = 0 // invalid configuration
	default:
		transportBitreservoir = 0
	}

	if audioMuxVersion == 2 {
		transportBitreservoir = minBufsizePerEffChan * nChannelsEff
	}

	return transportBitreservoir
}

// GetBitReservoirState mirrors FDKaacEnc_GetBitReservoirState()
// (aacenc.cpp:326): a thin forwarder to EncBitresToTpBitres for the same
// handle fields.
func GetBitReservoirState(bitrateMode BitrateMode, bitResTot, nChannelsEff, audioMuxVersion int) int {
	return EncBitresToTpBitres(bitrateMode, bitResTot, nChannelsEff, audioMuxVersion)
}

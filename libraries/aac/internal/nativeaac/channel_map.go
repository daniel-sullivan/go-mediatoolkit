// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// Channel-map init/config tier: the 1:1 port of the FDK-AAC encoder
// channel-mapping setup (libAACenc/src/channel_map.cpp). These functions decide
// the per-access-unit element layout (which SCE/CPE/LFE elements exist, which
// input channel feeds each coder channel, and the per-element relative-bits /
// bit budget split) before the per-frame psy + rate-control loops run.
//
// The channelModeConfig[] ROM and FDKaacEnc_GetChannelModeConfiguration /
// FDKaacEnc_GetMonoStereoMode live in ratecontrol_types.go (getChannelModeConfiguration
// / getMonoStereoMode); this file reuses them rather than re-declaring. The
// CHANNEL_MAPPING / ELEMENT_INFO / ELEMENT_BITS / QC_STATE structs live in
// bitenc_types.go + qc_main_types.go and are reused as-is.
//
// Everything here is pure integer arithmetic (FIXP_DBL == int32, INT == int):
// the relative-bits constants are exact FL2FXCONST_DBL literals and the bit
// splits are the fMult/CountLeadingBits int kernels, so the values are
// bit-identical regardless of build tag.

// --- aacenc.h:205: CHANNEL_ORDER -------------------------------------------

// ChannelOrder mirrors CHANNEL_ORDER (aacenc.h:205): the input-channel ordering
// convention. The AAC-LC encoder uses CH_ORDER_MPEG (aacenc.cpp:361).
type ChannelOrder int

const (
	ChOrderMPEG ChannelOrder = 0 // CH_ORDER_MPEG
	ChOrderWAV  ChannelOrder = 1 // CH_ORDER_WAV
	ChOrderWG4  ChannelOrder = 2 // CH_ORDER_WG4
)

// maxvalDBLI is MAXVAL_DBL as an int (common_fix.h:155, 0x7FFFFFFF): the
// "single-element gets all bits" relativeBits sentinel. The int32 form is
// maxvalDBL (block_switch.go); this int alias matches the C
// `(FIXP_DBL)MAXVAL_DBL` argument passed to FDKaacEnc_initElement.
const maxvalDBLI = 0x7FFFFFFF

// --- syslib_channelMapDescr.cpp:119-156: default channel map ROM ------------

// chMapInfo mirrors CHANNEL_MAP_INFO (syslib_channelMapDescr.h): one channel
// config's coder-channel -> input-channel map and its channel count.
type chMapInfo struct {
	channelMap  []byte // pChannelMap
	numChannels int    // numChannels
}

// mapFallback is the identity map (syslib_channelMapDescr.cpp:119), used for the
// channel configs without a dedicated map.
var mapFallback = []byte{
	0, 1, 2, 3, 4, 5, 6, 7,
	8, 9, 10, 11, 12, 13, 14, 15,
	16, 17, 18, 19, 20, 21, 22, 23,
}

// The per-config maps (syslib_channelMapDescr.cpp:122-134).
var (
	mapCfg1  = []byte{0, 1}
	mapCfg2  = []byte{0, 1}
	mapCfg3  = []byte{2, 0, 1}
	mapCfg4  = []byte{2, 0, 1, 3}
	mapCfg5  = []byte{2, 0, 1, 3, 4}
	mapCfg6  = []byte{2, 0, 1, 4, 5, 3}
	mapCfg7  = []byte{2, 6, 7, 0, 1, 4, 5, 3}
	mapCfg11 = []byte{2, 0, 1, 4, 5, 6, 3}
	mapCfg12 = []byte{2, 0, 1, 6, 7, 4, 5, 3}
	mapCfg13 = []byte{
		2, 6, 7, 0, 1, 10, 11, 4,
		5, 8, 3, 9, 14, 12, 13, 18,
		19, 15, 16, 17, 20, 21, 22, 23,
	}
	mapCfg14 = []byte{2, 0, 1, 4, 5, 3, 6, 7}
)

// dfltChMapTabLen is DFLT_CH_MAP_TAB_LEN (syslib_channelMapDescr.cpp:109, 15).
const dfltChMapTabLen = 15

// mapInfoTabDflt mirrors mapInfoTabDflt[] (syslib_channelMapDescr.cpp:140): the
// default coder->input channel map for each channel config 0..14.
var mapInfoTabDflt = [dfltChMapTabLen]chMapInfo{
	/*  0 */ {mapFallback, 24},
	/*  1 */ {mapCfg1, 2},
	/*  2 */ {mapCfg2, 2},
	/*  3 */ {mapCfg3, 3},
	/*  4 */ {mapCfg4, 4},
	/*  5 */ {mapCfg5, 5},
	/*  6 */ {mapCfg6, 6},
	/*  7 */ {mapCfg7, 8},
	/*  8 */ {mapFallback, 24},
	/*  9 */ {mapFallback, 24},
	/* 10 */ {mapFallback, 24},
	/* 11 */ {mapCfg11, 7},
	/* 12 */ {mapCfg12, 8},
	/* 13 */ {mapCfg13, 24},
	/* 14 */ {mapCfg14, 8},
}

// fdkChannelMapDescr mirrors FDK_channelMapDescr (syslib_channelMapDescr.h): the
// resolved channel-map descriptor used by chMapDescrGetMapValue.
type fdkChannelMapDescr struct {
	fPassThrough  int
	mapInfoTab    []chMapInfo
	mapInfoTabLen uint
}

// chMapDescrInit is the 1:1 port of FDK_chMapDescr_init
// (syslib_channelMapDescr.cpp:277). The channel-mapping path always passes
// pMapInfoTab=NULL / mapInfoTabLen=0, so useDefaultTab is always 1 and the
// default table is installed; only the fPassThrough flag varies with co.
//
// The custom-table validation branch (fdk_chMapDescr_isValid) is unreachable
// here (pMapInfoTab is always NULL) and is omitted; this faithfully reproduces
// the NULL-table call FDKaacEnc_InitChannelMapping makes.
func chMapDescrInit(d *fdkChannelMapDescr, fPassThrough uint) {
	if fPassThrough == 0 {
		d.fPassThrough = 0
	} else {
		d.fPassThrough = 1
	}
	// pMapInfoTab == NULL && mapInfoTabLen == 0 -> useDefaultTab stays 1.
	d.mapInfoTab = mapInfoTabDflt[:]
	d.mapInfoTabLen = dfltChMapTabLen
}

// chMapDescrGetMapValue is the 1:1 port of FDK_chMapDescr_getMapValue
// (syslib_channelMapDescr.cpp:192): pass chIdx through unless a non-passthrough
// custom map covers (chIdx, mapIdx).
//
//	UCHAR mapValue = chIdx; // pass through by default
//	if (fPassThrough==0 && pMapInfoTab!=NULL && mapInfoTabLen>mapIdx)
//	  if (chIdx < pMapInfoTab[mapIdx].numChannels)
//	    mapValue = pMapInfoTab[mapIdx].pChannelMap[chIdx];
func chMapDescrGetMapValue(d *fdkChannelMapDescr, chIdx int, mapIdx uint) int {
	mapValue := chIdx // pass through by default (UCHAR)
	if d.fPassThrough == 0 && d.mapInfoTab != nil && d.mapInfoTabLen > mapIdx {
		if chIdx < d.mapInfoTab[mapIdx].numChannels {
			mapValue = int(d.mapInfoTab[mapIdx].channelMap[chIdx])
		}
	}
	return mapValue
}

// --- channel_map.cpp:157: FDKaacEnc_DetermineEncoderMode --------------------

// DetermineEncoderMode is the 1:1 port of FDKaacEnc_DetermineEncoderMode
// (channel_map.cpp:157): resolve / validate the encoder CHANNEL_MODE against the
// requested channel count. When *mode is MODE_UNKNOWN it is looked up by channel
// count in channelModeConfig[]; otherwise the requested mode is validated. It
// returns the (possibly resolved) mode and an error code.
//
// Mirrors the C signature `AAC_ENCODER_ERROR(CHANNEL_MODE* mode, INT nChannels)`
// returning the resolved mode via the first return value.
func DetermineEncoderMode(mode ChannelMode, nChannels int) (ChannelMode, EncoderError) {
	encMode := ChannelModeInvalid

	if mode == ChannelModeUnknown {
		for i := range channelModeConfig {
			if channelModeConfig[i].nChannels == nChannels {
				encMode = channelModeConfig[i].encMode
				break
			}
		}
		mode = encMode
	} else {
		// check if valid channel configuration
		if getChannelModeConfiguration(mode).nChannels == nChannels {
			encMode = mode
		}
	}

	if encMode == ChannelModeInvalid {
		return mode, AacEncUnsupportedChannelconf
	}

	return mode, AacEncOK
}

// --- channel_map.cpp:186: FDKaacEnc_initElement -----------------------------

// initElement is the 1:1 port of the static FDKaacEnc_initElement
// (channel_map.cpp:186): fill one ELEMENT_INFO from the element type, advancing
// the coder-channel counter (*cnt) and the per-type instance-tag counter
// (itCnt[elType]). Returns the error flag (1 on an unknown element type).
//
// ID_CCE is folded into the SCE/LFE single-channel case exactly as in C; the
// AAC-LC encoder only emits ID_SCE/ID_CPE/ID_LFE, but the full switch is ported
// 1:1.
func initElement(elInfo *ElementInfo, elType int, cnt *int, mapDescr *fdkChannelMapDescr,
	mapIdx uint, itCnt []int, relBits int32) int {
	error := 0
	counter := *cnt

	elInfo.ElType = elType
	elInfo.RelativeBits = relBits

	switch elInfo.ElType {
	case IDSCE, IDLFE, IDCCE:
		elInfo.NChannelsInEl = 1
		elInfo.ChannelIndex[0] = chMapDescrGetMapValue(mapDescr, counter, mapIdx)
		counter++
		elInfo.InstanceTag = itCnt[elType]
		itCnt[elType]++
	case IDCPE:
		elInfo.NChannelsInEl = 2
		elInfo.ChannelIndex[0] = chMapDescrGetMapValue(mapDescr, counter, mapIdx)
		counter++
		elInfo.ChannelIndex[1] = chMapDescrGetMapValue(mapDescr, counter, mapIdx)
		counter++
		elInfo.InstanceTag = itCnt[elType]
		itCnt[elType]++
	case IDDSE:
		elInfo.NChannelsInEl = 0
		elInfo.ChannelIndex[0] = 0
		elInfo.ChannelIndex[1] = 0
		elInfo.InstanceTag = itCnt[elType]
		itCnt[elType]++
	default:
		error = 1
	}
	*cnt = counter
	return error
}

// --- channel_map.cpp:226: FDKaacEnc_InitChannelMapping ----------------------

// InitChannelMapping is the 1:1 port of FDKaacEnc_InitChannelMapping
// (channel_map.cpp:226): zero the CHANNEL_MAPPING, copy the matching
// channelModeConfig[] entry, build the channel-map descriptor, and lay out the
// per-mode ELEMENT_INFO list (with the AAC relative-bits split). It mirrors the
// C signature returning AAC_ENCODER_ERROR.
func InitChannelMapping(mode ChannelMode, co ChannelOrder, cm *ChannelMapping) EncoderError {
	count := 0                    // count through coder channels
	itCnt := make([]int, IDEND+1) // it_cnt[ID_END + 1], zeroed
	var mapDescr fdkChannelMapDescr

	*cm = ChannelMapping{} // FDKmemclear(cm, sizeof(CHANNEL_MAPPING))

	// init channel mapping
	for i := range channelModeConfig {
		if channelModeConfig[i].encMode == mode {
			cm.EncMode = channelModeConfig[i].encMode
			cm.NChannels = channelModeConfig[i].nChannels
			cm.NChannelsEff = channelModeConfig[i].nChannelsEff
			cm.NElements = channelModeConfig[i].nElements
			break
		}
	}

	// init map descriptor
	var passThrough uint
	if co == ChOrderMPEG {
		passThrough = 1
	}
	chMapDescrInit(&mapDescr, passThrough)

	var mapIdx uint
	switch mode {
	case ChannelMode7_1RearSurr: // equivalent to MODE_7_1_BACK
		mapIdx = uint(ChannelMode7_1Back)
	case ChannelMode7_1FrontCent: // equivalent to MODE_1_2_2_2_1
		mapIdx = uint(ChannelMode1_2_2_2_1)
	default:
		if int(mode) > 14 { // if channel config > 14 MPEG mapping will be used
			mapIdx = 0
		} else {
			mapIdx = uint(mode)
		}
	}

	// init element info struct
	switch mode {
	case ChannelMode1:
		// (mono) sce
		initElement(&cm.ElInfo[0], IDSCE, &count, &mapDescr, mapIdx, itCnt, maxvalDBLI)
	case ChannelMode2:
		// (stereo) cpe
		initElement(&cm.ElInfo[0], IDCPE, &count, &mapDescr, mapIdx, itCnt, maxvalDBLI)

	case ChannelMode1_2:
		// sce + cpe
		initElement(&cm.ElInfo[0], IDSCE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.4))
		initElement(&cm.ElInfo[1], IDCPE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.6))

	case ChannelMode1_2_1:
		// sce + cpe + sce
		initElement(&cm.ElInfo[0], IDSCE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.3))
		initElement(&cm.ElInfo[1], IDCPE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.4))
		initElement(&cm.ElInfo[2], IDSCE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.3))

	case ChannelMode1_2_2:
		// sce + cpe + cpe
		initElement(&cm.ElInfo[0], IDSCE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.26))
		initElement(&cm.ElInfo[1], IDCPE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.37))
		initElement(&cm.ElInfo[2], IDCPE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.37))

	case ChannelMode1_2_2_1:
		// (5.1) sce + cpe + cpe + lfe
		initElement(&cm.ElInfo[0], IDSCE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.24))
		initElement(&cm.ElInfo[1], IDCPE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.35))
		initElement(&cm.ElInfo[2], IDCPE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.35))
		initElement(&cm.ElInfo[3], IDLFE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.06))

	case ChannelMode6_1:
		// (6.1) sce + cpe + cpe + sce + lfe
		initElement(&cm.ElInfo[0], IDSCE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.2))
		initElement(&cm.ElInfo[1], IDCPE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.275))
		initElement(&cm.ElInfo[2], IDCPE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.275))
		initElement(&cm.ElInfo[3], IDSCE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.2))
		initElement(&cm.ElInfo[4], IDLFE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.05))

	case ChannelMode1_2_2_2_1,
		ChannelMode7_1Back,
		ChannelMode7_1TopFront,
		ChannelMode7_1RearSurr,
		ChannelMode7_1FrontCent:
		// (7.1) sce + cpe + cpe + cpe + lfe
		// (7.1 top) sce + cpe + cpe + lfe + cpe
		initElement(&cm.ElInfo[0], IDSCE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.18))
		initElement(&cm.ElInfo[1], IDCPE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.26))
		initElement(&cm.ElInfo[2], IDCPE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.26))
		if mode != ChannelMode7_1TopFront {
			initElement(&cm.ElInfo[3], IDCPE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.26))
			initElement(&cm.ElInfo[4], IDLFE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.04))
		} else {
			initElement(&cm.ElInfo[3], IDLFE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.04))
			initElement(&cm.ElInfo[4], IDCPE, &count, &mapDescr, mapIdx, itCnt, fl2fxconstDBLf(0.26))
		}

	default:
		return AacEncUnsupportedChannelconf
	}

	// FDK_ASSERT(cm->nElements <= 8) — invariant, not a runtime check.
	return AacEncOK
}

// --- channel_map.cpp:377: FDKaacEnc_InitElementBits -------------------------

// InitElementBits is the 1:1 port of FDKaacEnc_InitElementBits
// (channel_map.cpp:377): split the total bitrate and the per-channel max-bits
// budget across the access unit's elements according to their relativeBits,
// filling QC_STATE.elementBits[i] (chBitrateEl / maxBitsEl / relativeBitsEl).
// maxChannelBits is passed by value (C takes INT, modified locally for the LFE
// modes). Returns AAC_ENCODER_ERROR.
func InitElementBits(hQC *QcState, cm *ChannelMapping, bitrateTot, averageBitsTot,
	maxChannelBits int) EncoderError {
	scBrTot := int(fNorm(int32(bitrateTot))) // CountLeadingBits(bitrateTot)

	switch cm.EncMode {
	case ChannelMode1:
		hQC.ElementBits[0].ChBitrateEl = bitrateTot
		hQC.ElementBits[0].MaxBitsEl = maxChannelBits
		hQC.ElementBits[0].RelativeBitsEl = cm.ElInfo[0].RelativeBits

	case ChannelMode2:
		hQC.ElementBits[0].ChBitrateEl = bitrateTot >> 1
		hQC.ElementBits[0].MaxBitsEl = 2 * maxChannelBits
		hQC.ElementBits[0].RelativeBitsEl = cm.ElInfo[0].RelativeBits

	case ChannelMode1_2:
		hQC.ElementBits[0].RelativeBitsEl = cm.ElInfo[0].RelativeBits
		hQC.ElementBits[1].RelativeBitsEl = cm.ElInfo[1].RelativeBits
		sceRate := cm.ElInfo[0].RelativeBits
		cpeRate := cm.ElInfo[1].RelativeBits

		hQC.ElementBits[0].ChBitrateEl =
			int(fMult(sceRate, int32(bitrateTot<<scBrTot)) >> scBrTot)
		hQC.ElementBits[1].ChBitrateEl =
			int(fMult(cpeRate, int32(bitrateTot<<scBrTot)) >> (scBrTot + 1))

		hQC.ElementBits[0].MaxBitsEl = maxChannelBits
		hQC.ElementBits[1].MaxBitsEl = 2 * maxChannelBits

	case ChannelMode1_2_1:
		// sce + cpe + sce
		hQC.ElementBits[0].RelativeBitsEl = cm.ElInfo[0].RelativeBits
		hQC.ElementBits[1].RelativeBitsEl = cm.ElInfo[1].RelativeBits
		hQC.ElementBits[2].RelativeBitsEl = cm.ElInfo[2].RelativeBits
		sce1Rate := cm.ElInfo[0].RelativeBits
		cpeRate := cm.ElInfo[1].RelativeBits
		sce2Rate := cm.ElInfo[2].RelativeBits

		hQC.ElementBits[0].ChBitrateEl =
			int(fMult(sce1Rate, int32(bitrateTot<<scBrTot)) >> scBrTot)
		hQC.ElementBits[1].ChBitrateEl =
			int(fMult(cpeRate, int32(bitrateTot<<scBrTot)) >> (scBrTot + 1))
		hQC.ElementBits[2].ChBitrateEl =
			int(fMult(sce2Rate, int32(bitrateTot<<scBrTot)) >> scBrTot)

		hQC.ElementBits[0].MaxBitsEl = maxChannelBits
		hQC.ElementBits[1].MaxBitsEl = 2 * maxChannelBits
		hQC.ElementBits[2].MaxBitsEl = maxChannelBits

	case ChannelMode1_2_2:
		// sce + cpe + cpe
		hQC.ElementBits[0].RelativeBitsEl = cm.ElInfo[0].RelativeBits
		hQC.ElementBits[1].RelativeBitsEl = cm.ElInfo[1].RelativeBits
		hQC.ElementBits[2].RelativeBitsEl = cm.ElInfo[2].RelativeBits
		sceRate := cm.ElInfo[0].RelativeBits
		cpe1Rate := cm.ElInfo[1].RelativeBits
		cpe2Rate := cm.ElInfo[2].RelativeBits

		hQC.ElementBits[0].ChBitrateEl =
			int(fMult(sceRate, int32(bitrateTot<<scBrTot)) >> scBrTot)
		hQC.ElementBits[1].ChBitrateEl =
			int(fMult(cpe1Rate, int32(bitrateTot<<scBrTot)) >> (scBrTot + 1))
		hQC.ElementBits[2].ChBitrateEl =
			int(fMult(cpe2Rate, int32(bitrateTot<<scBrTot)) >> (scBrTot + 1))

		hQC.ElementBits[0].MaxBitsEl = maxChannelBits
		hQC.ElementBits[1].MaxBitsEl = 2 * maxChannelBits
		hQC.ElementBits[2].MaxBitsEl = 2 * maxChannelBits

	case ChannelMode1_2_2_1:
		// (5.1) sce + cpe + cpe + lfe
		hQC.ElementBits[0].RelativeBitsEl = cm.ElInfo[0].RelativeBits
		hQC.ElementBits[1].RelativeBitsEl = cm.ElInfo[1].RelativeBits
		hQC.ElementBits[2].RelativeBitsEl = cm.ElInfo[2].RelativeBits
		hQC.ElementBits[3].RelativeBitsEl = cm.ElInfo[3].RelativeBits
		sceRate := cm.ElInfo[0].RelativeBits
		cpe1Rate := cm.ElInfo[1].RelativeBits
		cpe2Rate := cm.ElInfo[2].RelativeBits
		lfeRate := cm.ElInfo[3].RelativeBits

		maxBitsTot := maxChannelBits * 5 // LFE does not add to bit reservoir
		sc := int(fNorm(int32(fixMax(maxChannelBits, averageBitsTot))))
		maxLfeBits := int(fMax(
			int32((fMult(lfeRate, int32(maxChannelBits<<sc))>>sc)<<1),
			int32(((fMult(fl2fxconstDBLf(1.1/2.0),
				fMult(lfeRate, int32(averageBitsTot<<sc))) << 1) >> sc))))

		maxChannelBits = maxBitsTot - maxLfeBits
		sc = int(fNorm(int32(maxChannelBits)))

		maxChannelBits = int(fMult(int32(maxChannelBits)<<sc, getInvInt(5)) >> sc)

		hQC.ElementBits[0].ChBitrateEl =
			int(fMult(sceRate, int32(bitrateTot<<scBrTot)) >> scBrTot)
		hQC.ElementBits[1].ChBitrateEl =
			int(fMult(cpe1Rate, int32(bitrateTot<<scBrTot)) >> (scBrTot + 1))
		hQC.ElementBits[2].ChBitrateEl =
			int(fMult(cpe2Rate, int32(bitrateTot<<scBrTot)) >> (scBrTot + 1))
		hQC.ElementBits[3].ChBitrateEl =
			int(fMult(lfeRate, int32(bitrateTot<<scBrTot)) >> scBrTot)

		hQC.ElementBits[0].MaxBitsEl = maxChannelBits
		hQC.ElementBits[1].MaxBitsEl = 2 * maxChannelBits
		hQC.ElementBits[2].MaxBitsEl = 2 * maxChannelBits
		hQC.ElementBits[3].MaxBitsEl = maxLfeBits

	case ChannelMode6_1:
		// (6.1) sce + cpe + cpe + sce + lfe
		sceRate := cm.ElInfo[0].RelativeBits
		hQC.ElementBits[0].RelativeBitsEl = sceRate
		cpe1Rate := cm.ElInfo[1].RelativeBits
		hQC.ElementBits[1].RelativeBitsEl = cpe1Rate
		cpe2Rate := cm.ElInfo[2].RelativeBits
		hQC.ElementBits[2].RelativeBitsEl = cpe2Rate
		sce2Rate := cm.ElInfo[3].RelativeBits
		hQC.ElementBits[3].RelativeBitsEl = sce2Rate
		lfeRate := cm.ElInfo[4].RelativeBits
		hQC.ElementBits[4].RelativeBitsEl = lfeRate

		maxBitsTot := maxChannelBits * 6 // LFE does not add to bit reservoir
		sc := int(fNorm(int32(fixMax(maxChannelBits, averageBitsTot))))
		maxLfeBits := int(fMax(
			int32((fMult(lfeRate, int32(maxChannelBits<<sc))>>sc)<<1),
			int32(((fMult(fl2fxconstDBLf(1.1/2.0),
				fMult(lfeRate, int32(averageBitsTot<<sc))) << 1) >> sc))))

		maxChannelBits = (maxBitsTot - maxLfeBits) / 6

		hQC.ElementBits[0].ChBitrateEl =
			int(fMult(sceRate, int32(bitrateTot<<scBrTot)) >> scBrTot)
		hQC.ElementBits[1].ChBitrateEl =
			int(fMult(cpe1Rate, int32(bitrateTot<<scBrTot)) >> (scBrTot + 1))
		hQC.ElementBits[2].ChBitrateEl =
			int(fMult(cpe2Rate, int32(bitrateTot<<scBrTot)) >> (scBrTot + 1))
		hQC.ElementBits[3].ChBitrateEl =
			int(fMult(sce2Rate, int32(bitrateTot<<scBrTot)) >> (scBrTot + 1))
		hQC.ElementBits[4].ChBitrateEl =
			int(fMult(lfeRate, int32(bitrateTot<<scBrTot)) >> scBrTot)

		hQC.ElementBits[0].MaxBitsEl = maxChannelBits
		hQC.ElementBits[1].MaxBitsEl = 2 * maxChannelBits
		hQC.ElementBits[2].MaxBitsEl = 2 * maxChannelBits
		hQC.ElementBits[3].MaxBitsEl = maxChannelBits
		hQC.ElementBits[4].MaxBitsEl = maxLfeBits

	case ChannelMode7_1TopFront,
		ChannelMode7_1Back,
		ChannelMode7_1RearSurr,
		ChannelMode7_1FrontCent,
		ChannelMode1_2_2_2_1:
		cpe3Idx := 3
		lfeIdx := 4
		if cm.EncMode == ChannelMode7_1TopFront {
			cpe3Idx = 4
			lfeIdx = 3
		}

		// (7.1) sce + cpe + cpe + cpe + lfe
		sceRate := cm.ElInfo[0].RelativeBits
		hQC.ElementBits[0].RelativeBitsEl = sceRate
		cpe1Rate := cm.ElInfo[1].RelativeBits
		hQC.ElementBits[1].RelativeBitsEl = cpe1Rate
		cpe2Rate := cm.ElInfo[2].RelativeBits
		hQC.ElementBits[2].RelativeBitsEl = cpe2Rate
		cpe3Rate := cm.ElInfo[cpe3Idx].RelativeBits
		hQC.ElementBits[cpe3Idx].RelativeBitsEl = cpe3Rate
		lfeRate := cm.ElInfo[lfeIdx].RelativeBits
		hQC.ElementBits[lfeIdx].RelativeBitsEl = lfeRate

		maxBitsTot := maxChannelBits * 7 // LFE does not add to bit reservoir
		sc := int(fNorm(int32(fixMax(maxChannelBits, averageBitsTot))))
		maxLfeBits := int(fMax(
			int32((fMult(lfeRate, int32(maxChannelBits<<sc))>>sc)<<1),
			int32(((fMult(fl2fxconstDBLf(1.1/2.0),
				fMult(lfeRate, int32(averageBitsTot<<sc))) << 1) >> sc))))

		maxChannelBits = (maxBitsTot - maxLfeBits) / 7

		hQC.ElementBits[0].ChBitrateEl =
			int(fMult(sceRate, int32(bitrateTot<<scBrTot)) >> scBrTot)
		hQC.ElementBits[1].ChBitrateEl =
			int(fMult(cpe1Rate, int32(bitrateTot<<scBrTot)) >> (scBrTot + 1))
		hQC.ElementBits[2].ChBitrateEl =
			int(fMult(cpe2Rate, int32(bitrateTot<<scBrTot)) >> (scBrTot + 1))
		hQC.ElementBits[cpe3Idx].ChBitrateEl =
			int(fMult(cpe3Rate, int32(bitrateTot<<scBrTot)) >> (scBrTot + 1))
		hQC.ElementBits[lfeIdx].ChBitrateEl =
			int(fMult(lfeRate, int32(bitrateTot<<scBrTot)) >> scBrTot)

		hQC.ElementBits[0].MaxBitsEl = maxChannelBits
		hQC.ElementBits[1].MaxBitsEl = 2 * maxChannelBits
		hQC.ElementBits[2].MaxBitsEl = 2 * maxChannelBits
		hQC.ElementBits[cpe3Idx].MaxBitsEl = 2 * maxChannelBits
		hQC.ElementBits[lfeIdx].MaxBitsEl = maxLfeBits

	default:
		return AacEncUnsupportedChannelconf
	}

	return AacEncOK
}

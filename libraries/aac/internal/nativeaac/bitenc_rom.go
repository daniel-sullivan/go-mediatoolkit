// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

// Bitstream-encode area: the raw-data-block element list that
// FDKaacEnc_ChannelElementWrite walks.
//
// 1:1 port of the element_list_t parser-guidance ROM and getBitstreamElementList
// from FDK_tools_rom.cpp, restricted to the AOT {2,5,29} (AAC-LC / SBR / PS,
// epConfig = -1) nodes that the AAC-LC encode path uses: node_aac_sce,
// node_aac_cpe (+ cpe0/cpe1) and node_aac_cce. The ER / scalable / ELD / USAC /
// DRM nodes are a separate area and are intentionally not ported; for those
// AOTs getBitstreamElementList returns nil, which the caller surfaces as
// AAC_ENC_UNSUPPORTED_AOT exactly as the C does when list == NULL.
//
// Pure ROM + a switch — integer/data only, bit-identical in every build.

package nativeaac

// rbdID enumerates the raw-data-block list items (FDK_tools_rom.h:297,
// typedef enum { ... } rbd_id_t). Order matters: the C code compares against
// these symbolic values, never their numeric value, so only the subset the
// AAC-LC nodes reference plus the control items need to round-trip — but the
// full prefix is reproduced so the iota values match the C ordinals.
type rbdID int

const (
	elementInstanceTag rbdID = iota
	commonWindow
	globalGain
	icsInfo
	maxSfb
	ms
	ltpDataPresent
	ltpData
	sectionData
	scaleFactorData
	pulse
	tnsDataPresent
	tnsData
	gainControlDataPresent
	gainControlData
	esc1HCR
	esc2RVLC
	spectralData

	scaleFactorDataUsac
	coreMode
	commonTw
	lpdChannelStream
	twData
	noise
	acSpectralData
	facData
	tnsActive
	tnsDataPresentUsac
	commonMaxSfb

	coupledElements
	gainElementLists

	// Non data list items
	adtscrcStartReg1
	adtscrcStartReg2
	adtscrcEndReg1
	adtscrcEndReg2
	drmcrcStartReg
	drmcrcEndReg
	nextChannel
	nextChannelLoop
	linkSequence
	endOfSequence
)

// elementList is the parser-guidance node (FDK_tools_rom.h:348,
// struct element_list { const rbd_id_t *id; const struct element_list
// *next[2]; }).
type elementList struct {
	id   []rbdID
	next [2]*elementList
}

// AAC SCE — AOT {2,5,29}, epConfig = -1 (FDK_tools_rom.cpp:6547, el_aac_sce).
var elAacSce = []rbdID{
	adtscrcStartReg1, elementInstanceTag, globalGain, icsInfo,
	sectionData, scaleFactorData, pulse, tnsDataPresent, tnsData,
	gainControlDataPresent,
	spectralData, adtscrcEndReg1, endOfSequence,
}

var nodeAacSce = &elementList{id: elAacSce}

// AAC CCE (FDK_tools_rom.cpp:6557, el_aac_cce / node_aac_cce).
var elAacCce = []rbdID{
	adtscrcStartReg1, elementInstanceTag,
	coupledElements,
	globalGain, icsInfo, sectionData, scaleFactorData, pulse,
	tnsDataPresent, tnsData, gainControlDataPresent,
	spectralData, gainElementLists,
	adtscrcEndReg1, endOfSequence,
}

var nodeAacCce = &elementList{id: elAacCce}

// AAC CPE (FDK_tools_rom.cpp:6568, el_aac_cpe / el_aac_cpe0 / el_aac_cpe1).
var elAacCpe = []rbdID{
	adtscrcStartReg1, elementInstanceTag, commonWindow, linkSequence,
}

var elAacCpe0 = []rbdID{
	// common_window = 0
	globalGain, icsInfo, sectionData, scaleFactorData, pulse,
	tnsDataPresent, tnsData, gainControlDataPresent,
	spectralData, nextChannel,

	adtscrcStartReg2, globalGain, icsInfo, sectionData, scaleFactorData,
	pulse, tnsDataPresent, tnsData, gainControlDataPresent,
	spectralData, adtscrcEndReg1, adtscrcEndReg2, endOfSequence,
}

var elAacCpe1 = []rbdID{
	// common_window = 1
	icsInfo, ms,

	globalGain, sectionData, scaleFactorData, pulse, tnsDataPresent,
	tnsData, gainControlDataPresent,
	spectralData, nextChannel,

	adtscrcStartReg2, globalGain, sectionData, scaleFactorData, pulse,
	tnsDataPresent, tnsData, gainControlDataPresent,
	spectralData, adtscrcEndReg1, adtscrcEndReg2, endOfSequence,
}

var nodeAacCpe0 = &elementList{id: elAacCpe0}

var nodeAacCpe1 = &elementList{id: elAacCpe1}

var nodeAacCpe = &elementList{
	id:   elAacCpe,
	next: [2]*elementList{nodeAacCpe0, nodeAacCpe1},
}

// getBitstreamElementList returns the parser-guidance list for the given
// parameters (FDK_tools_rom.cpp:7133, getBitstreamElementList). Only the
// AOT {2,5,29} (epConfig = -1) cases are ported; other AOTs return nil.
func getBitstreamElementList(aot int, epConfig int, nChannels int, layer int, elFlags uint32) *elementList {
	switch aot {
	case AOTAACLC, AOTSBR, AOTPS:
		// FDK_ASSERT(epConfig == -1)
		if elFlags&acElGaCce != 0 {
			return nodeAacCce
		}
		if nChannels == 1 {
			return nodeAacSce
		}
		return nodeAacCpe
	default:
		return nil
	}
}

// acElGaCce is the GA coupling-channel-element flag (FDK_tools_rom.h:292,
// #define AC_EL_GA_CCE 0x00000001).
const acElGaCce = 0x00000001

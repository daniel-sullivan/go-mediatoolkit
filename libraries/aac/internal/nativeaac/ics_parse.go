// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// This file ports the AAC-LC raw_data_block "ics-parse" stage 1:1 from the
// vendored Fraunhofer FDK reference: the individual-channel-stream info read
// (IcsRead / IcsReadMaxSfb, channelinfo.cpp) — window sequence/shape, max-sfb,
// scalefactor-window grouping — and the section-data read
// (CBlock_ReadSectionData, block.cpp) — the per-(group,band) codebook layout.
//
// These are pure-integer bitstream parse kernels: every value is a UCHAR/INT
// field unpacked MSB-first via the genuine FDK_BITSTREAM cache reader
// (bitstream.go). No float appears, so they are bit-identical regardless of
// build tag; the parity gate asserts EXACT int32 equality of every output
// (window structure, group lengths, codebook array, return code) against the
// vendored C compiled into the cgo oracle.

// Audio-codec flags (the AC_* bitmask passed to IcsRead / CBlock_ReadSectionData
// as `flags`). 1:1 from libSYS/include/FDK_audio.h:293. Only the bits the ported
// kernels test are transcribed; AAC-LC passes flags == 0.
const (
	acErVCB11  = 0x000001 // AC_ER_VCB11   (FDK_audio.h:293)
	acErHCR    = 0x000004 // AC_ER_HCR     (FDK_audio.h:299)
	acScalable = 0x000008 // AC_SCALABLE   (FDK_audio.h:302)
	acELD      = 0x000010 // AC_ELD        (FDK_audio.h:303)
	acLD       = 0x000020 // AC_LD         (FDK_audio.h:304)
	acBSAC     = 0x000080 // AC_BSAC       (FDK_audio.h:306)
	acUSAC     = 0x000100 // AC_USAC       (FDK_audio.h:307)
	acRSV603DA = 0x000200 // AC_RSV603DA   (FDK_audio.h:308)
	acRSVD50   = 0x004000 // AC_RSVD50     (FDK_audio.h:310)
	acMPEGDRes = 0x200000 // AC_MPEGD_RES  (FDK_audio.h:320)
)

// blockType ports the BLOCK_TYPE enum (libFDK/include/mdct.h:124).
type blockType uint8

const (
	blockLong  blockType = 0 // BLOCK_LONG  — normal long block
	blockStart blockType = 1 // BLOCK_START — long start block
	blockShort blockType = 2 // BLOCK_SHORT — 8 short blocks sequence
	blockStop  blockType = 3 // BLOCK_STOP  — long stop block
)

// Additional AAC_DECODER_ERROR codes the ics-parse stage can return
// (libAACdec/include/aacdecoder_lib.h:443). aacDecOK / aacDecUnsupportedFormat
// are declared in aac_rom_sfb.go.
const (
	aacDecParseError             aacDecoderError = 0x4002 // AAC_DEC_PARSE_ERROR
	aacDecDecodeFrameError       aacDecoderError = 0x4004 // AAC_DEC_DECODE_FRAME_ERROR
	aacDecInvalidCodeBook        aacDecoderError = 0x4006 // AAC_DEC_INVALID_CODE_BOOK
	aacDecUnsupportedPrediction  aacDecoderError = 0x4007 // AAC_DEC_UNSUPPORTED_PREDICTION
	aacDecTnsReadError           aacDecoderError = 0x400C // AAC_DEC_TNS_READ_ERROR
	aacDecUnsupportedGainControl aacDecoderError = 0x400A // AAC_DEC_UNSUPPORTED_GAIN_CONTROL_DATA
)

// bookscl is BOOKSCL == NSPECBOOKS == ESCBOOK+1 (channelinfo.h:185): the
// scalefactor codebook index, illegal as a section codebook.
const bookscl = escBook + 1 // 12

// maxSfbHcr ports MAX_SFB_HCR == (((1024/8)/LINES_PER_UNIT)*8) == 256
// (LINES_PER_UNIT == 4). CBlock_ReadSectionData bounds the HCR side-info index
// with it; AAC-LC (flags without AC_ER_HCR) never enters that branch, but the
// constant is transcribed for the 1:1 bound check.
//
// C counterpart: libAACdec/src/aacdec_hcr_types.h:119.
const maxSfbHcr = ((1024 / 8) / 4) * 8 // 256

// cIcsInfo ports the C CIcsInfo struct (channelinfo.h:167): the parsed
// individual-channel-stream info — window structure and scalefactor-band counts.
// Field names and integer widths mirror the C struct so the parity oracle can
// compare each field bit-for-bit.
type cIcsInfo struct {
	windowGroupLength   [8]uint8  // UCHAR WindowGroupLength[8]
	windowGroups        uint8     // UCHAR WindowGroups
	valid               uint8     // UCHAR Valid
	windowShape         uint8     // UCHAR WindowShape (0 sine, 1 KBD, 2 low overlap)
	windowSequence      blockType // BLOCK_TYPE WindowSequence
	maxSfBands          uint8     // UCHAR MaxSfBands
	maxSfbSte           uint8     // UCHAR max_sfb_ste
	scaleFactorGrouping uint8     // UCHAR ScaleFactorGrouping
	totalSfBands        uint8     // UCHAR TotalSfBands
}

// isLongBlock ports IsLongBlock (channelinfo.h:499): a block is "long" for every
// window sequence except BLOCK_SHORT.
func isLongBlock(p *cIcsInfo) bool { return p.windowSequence != blockShort }

// getWindowGroups ports GetWindowGroups (channelinfo.h:533).
func getWindowGroups(p *cIcsInfo) int { return int(p.windowGroups) }

// getWindowGroupLength ports GetWindowGroupLength (channelinfo.h:537).
func getWindowGroupLength(p *cIcsInfo, index int) int { return int(p.windowGroupLength[index]) }

// getScaleFactorBandsTransmitted ports GetScaleFactorBandsTransmitted
// (channelinfo.h:545).
func getScaleFactorBandsTransmitted(p *cIcsInfo) int { return int(p.maxSfBands) }

// getNumberOfScaleFactorBands ports GetNumberOfScaleFactorBands
// (channelinfo.h:520): the total band count for the active block length.
func getNumberOfScaleFactorBands(p *cIcsInfo, sri *samplingRateInfo) int {
	if isLongBlock(p) {
		return int(sri.numberOfScaleFactorBandsLong)
	}
	return int(sri.numberOfScaleFactorBandsShort)
}

// getScaleFactorBandOffsets ports GetScaleFactorBandOffsets (channelinfo.h:511):
// the scalefactor-band line-offset table for the active block length.
func getScaleFactorBandOffsets(p *cIcsInfo, sri *samplingRateInfo) []int16 {
	if isLongBlock(p) {
		return sri.scaleFactorBandsLong
	}
	return sri.scaleFactorBandsShort
}

// icsReadMaxSfb ports IcsReadMaxSfb (channelinfo.cpp:108): read max_sfb (6 bits
// for long blocks, 4 for short), set TotalSfBands from the sampling-rate info,
// and flag a parse error if MaxSfBands exceeds the total band count.
func icsReadMaxSfb(bs *bitStream, p *cIcsInfo, sri *samplingRateInfo) aacDecoderError {
	errorStatus := aacDecOK
	var nbits uint32

	if isLongBlock(p) {
		nbits = 6
		p.totalSfBands = sri.numberOfScaleFactorBandsLong
	} else {
		nbits = 4
		p.totalSfBands = sri.numberOfScaleFactorBandsShort
	}
	p.maxSfBands = uint8(bs.readBits(nbits))

	if p.maxSfBands > p.totalSfBands {
		errorStatus = aacDecParseError
	}

	return errorStatus
}

// icsRead ports IcsRead (channelinfo.cpp:129): read the window sequence/shape,
// max_sfb, and (for short blocks) the scalefactor-window grouping that derives
// the window-group count and per-group lengths. flags is the AC_* bitmask; for
// AAC-LC it is 0. Returns the C AAC_DECODER_ERROR; on success Valid is set to 1.
//
// The C path for the unsupported syntaxes (ELD/LD/Scalable/BSAC/USAC/RSVD) is
// transcribed verbatim so flag-gated reads stay bit-identical, even though the
// AAC-LC caller never sets those bits.
func icsRead(bs *bitStream, p *cIcsInfo, sri *samplingRateInfo, flags uint32) aacDecoderError {
	errorStatus := aacDecOK

	p.valid = 0

	if flags&acELD != 0 {
		p.windowSequence = blockLong
		p.windowShape = 0
	} else {
		if flags&(acUSAC|acRSVD50|acRSV603DA) == 0 {
			bs.readBits(1)
		}
		p.windowSequence = blockType(bs.readBits(2))
		p.windowShape = uint8(bs.readBits(1))
		if flags&acLD != 0 {
			if p.windowShape != 0 {
				p.windowShape = 2 // select low overlap instead of KBD
			}
		}
	}

	// Sanity check (channelinfo.cpp:153).
	if flags&(acELD|acLD) != 0 && p.windowSequence != blockLong {
		p.windowSequence = blockLong
		errorStatus = aacDecParseError
		goto bail
	}

	errorStatus = icsReadMaxSfb(bs, p, sri)
	if errorStatus != aacDecOK {
		goto bail
	}

	if isLongBlock(p) {
		if flags&(acELD|acScalable|acBSAC|acUSAC|acRSVD50|acRSV603DA) == 0 {
			// If not ELD nor Scalable nor BSAC nor USAC syntax then read
			// predictor_data_present (must be 0 for AAC-LC).
			if uint8(bs.readBits(1)) != 0 { // UCHAR PredictorDataPresent
				errorStatus = aacDecUnsupportedPrediction
				goto bail
			}
		}

		p.windowGroups = 1
		p.windowGroupLength[0] = 1
	} else {
		p.scaleFactorGrouping = uint8(bs.readBits(7))

		p.windowGroups = 0

		for i := 0; i < (8 - 1); i++ {
			mask := uint32(1) << uint(6-i)
			p.windowGroupLength[i] = 1

			if uint32(p.scaleFactorGrouping)&mask != 0 {
				p.windowGroupLength[p.windowGroups]++
			} else {
				p.windowGroups++
			}
		}

		// loop runs to i < 7 only
		p.windowGroupLength[8-1] = 1
		p.windowGroups++
	}

bail:
	if errorStatus == aacDecOK {
		p.valid = 1
	}

	return errorStatus
}

// cSectionData holds the outputs of readSectionData that the rest of the decode
// chain consumes. aCodeBook is the flat per-(group*16+band) section codebook
// array (pDynData->aCodeBook, sized 8*16); numberSection mirrors
// specificTo.aac.numberSection (only advanced on the HCR path).
type cSectionData struct {
	aCodeBook     [8 * 16]uint8
	numberSection int
}

// readSectionData ports CBlock_ReadSectionData (block.cpp:326): for every window
// group, walk the transmitted scalefactor bands reading (codebook, run-length)
// section pairs — sect_cb is 4 bits (5 with AC_ER_VCB11), sect_len is run in
// nbits-wide increments (5 for long, 3 for short) with an all-ones escape — and
// stamp sect_cb across [band, band+sect_len) of the flat codebook array. Returns
// the C AAC_DECODER_ERROR.
//
// The HCR side-info collection branch (flags & AC_ER_HCR) is transcribed for the
// 1:1 bound/return checks but writes to nothing AAC-LC observes; AAC-LC passes
// flags == 0 and never enters it. p must already carry the cIcsInfo from icsRead
// and commonWindow is RawDataInfo.CommonWindow (gates the intensity-codebook
// legality check).
func readSectionData(bs *bitStream, p *cIcsInfo, sri *samplingRateInfo, commonWindow uint8, flags uint32, sd *cSectionData) aacDecoderError {
	var top, band int
	var sectLen, sectLenIncr int
	var sectCb uint8

	pCodeBook := &sd.aCodeBook
	bandOffsets := getScaleFactorBandOffsets(p, sri)
	sd.numberSection = 0
	numLinesInSecIdx := 0
	errorStatus := aacDecOK

	// FDKmemclear(pCodeBook, sizeof(UCHAR) * (8 * 16)).
	for i := range pCodeBook {
		pCodeBook[i] = 0
	}

	nbits := 3
	if isLongBlock(p) {
		nbits = 5
	}

	sectEscVal := (1 << uint(nbits)) - 1

	scaleFactorBandsTransmitted := getScaleFactorBandsTransmitted(p)
	for group := 0; group < getWindowGroups(p); group++ {
		for band = 0; band < scaleFactorBandsTransmitted; {
			sectLen = 0
			if flags&acErVCB11 != 0 {
				sectCb = uint8(bs.readBits(5))
			} else {
				sectCb = uint8(bs.readBits(4))
			}

			if (flags&acErVCB11) == 0 || sectCb < 11 || (sectCb > 11 && sectCb < 16) {
				sectLenIncr = int(bs.readBits(uint32(nbits)))
				for sectLenIncr == sectEscVal {
					sectLen += sectEscVal
					sectLenIncr = int(bs.readBits(uint32(nbits)))
				}
			} else {
				sectLenIncr = 1
			}

			sectLen += sectLenIncr

			top = band + sectLen

			if flags&acErHCR != 0 {
				// HCR input (long) -- collecting sideinfo (HCR _long_ only).
				if numLinesInSecIdx >= maxSfbHcr {
					return aacDecParseError
				}
				if top > getNumberOfScaleFactorBands(p, sri) {
					return aacDecParseError
				}
				// pNumLinesInSec[idx] = BandOffsets[top] - BandOffsets[band]
				// (collected for HCR; AAC-LC never reaches here).
				_ = bandOffsets
				numLinesInSecIdx++
				if int(sectCb) == bookscl {
					return aacDecInvalidCodeBook
				}
				// *pHcrCodeBook++ = sect_cb (HCR-only output, not observed here).
				sd.numberSection++
			}

			// Check spectral line limits (block.cpp:398).
			if isLongBlock(p) {
				if top > 64 {
					return aacDecDecodeFrameError
				}
			} else { // short block
				if top+group*16 > (8 * 16) {
					return aacDecDecodeFrameError
				}
			}

			// Check if decoded codebook index is feasible (block.cpp:409).
			if int(sectCb) == bookscl ||
				((int(sectCb) == intensityHCB || int(sectCb) == intensityHCB2) && commonWindow == 0) {
				return aacDecInvalidCodeBook
			}

			// Store codebook index.
			for ; band < top; band++ {
				pCodeBook[group*16+band] = sectCb
			}
		}
	}

	return errorStatus
}

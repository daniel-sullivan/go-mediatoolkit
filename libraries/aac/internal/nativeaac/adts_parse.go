// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

// This file ports the ADTS frame-sync-parse feature: the syncword search and
// the fixed/variable header field parse. It is an integer kernel (only bit
// reads and integer arithmetic), so it is bit-identical regardless of
// vectorization; parity is EXACT integer equality (there is no FP path in the
// AAC port — see nativeaac.go).

package nativeaac

// findSyncword searches the bitstream for the ADTS syncword and leaves the read
// position immediately past the matched syncword. 1:1 port of the ADTS branch of
// the syncword search loop in
// libfdk/libMpegTPDec/src/tpdec_lib.cpp:1118 (the TT_MP4_ADTS, fresh-frame
// numberOfRawDataBlocks==0 path with TPDEC_SYNCOK clear). Returns
// transportDecNotEnoughBits, transportDecSyncError or transportDecOK; on OK the
// syncLength syncword bits have been consumed.
func findSyncword(hBs *adtsBitReader) transportDecError {
	const syncWord = adtsSyncword
	const syncLength = adtsSyncLength
	syncMask := uint32((1 << syncLength) - 1)

	bitsAvail := hBs.getValidBits()

	// if ((bitsAvail - syncLength) < TPDEC_SYNCSKIP) — not enough to search.
	if (bitsAvail - syncLength) < tpdecSyncSkip {
		return transportDecNotEnoughBits
	}

	synch := hBs.readBits(syncLength)
	for ; (bitsAvail - syncLength) >= tpdecSyncSkip; bitsAvail -= tpdecSyncSkip {
		if synch == syncWord {
			break
		}
		synch = ((synch << tpdecSyncSkip) & syncMask) | hBs.readBits(tpdecSyncSkip)
	}
	if synch != syncWord {
		return transportDecSyncError
	}
	return transportDecOK
}

// decodeHeader parses one ADTS header at the current bitstream position,
// filling pAdts with the parsed fields. 1:1 port of adtsRead_DecodeHeader in
// libfdk/libMpegTPDec/src/tpdec_adts.cpp:166.
//
// The slice owns the integer field parse, the frame_length / num_raw_blocks
// accounting and the buffer-fullness gate. The CRC region calls
// (FDKcrcStartReg/EndReg/Reset, the protection_absent crc_check read) belong to
// the crc area, and the channel_config==0 PCE branch
// (CProgramConfig_Read/Compare) plus AudioSpecificConfig_Init belong to the
// pce-asc area; those cross-area calls are represented by the seam helpers
// below so the control flow stays a faithful 1:1 mirror. ignoreBufferFullness
// matches the C parameter of the same name.
//
// Note: the syncword has already been consumed by findSyncword, so on entry
// getValidBits() does NOT include the 12 syncword bits; the C code restores them
// via `valBits = FDKgetValidBits(hBs) + ADTS_SYNCLENGTH`.
func decodeHeader(pAdts *adts, hBs *adtsBitReader, ignoreBufferFullness bool) transportDecError {
	var bs adtsBS

	// valBits = FDKgetValidBits(hBs) + ADTS_SYNCLENGTH;
	valBits := hBs.getValidBits() + adtsSyncLength

	if valBits < adtsHeaderLength {
		return transportDecNotEnoughBits
	}

	// adts_fixed_header
	bs.mpegID = uint8(hBs.readBits(adtsLengthID))
	bs.layer = uint8(hBs.readBits(adtsLengthLayer))
	bs.protectionAbsent = uint8(hBs.readBits(adtsLengthProtectionAbsent))
	bs.profile = uint8(hBs.readBits(adtsLengthProfile))
	bs.sampleFreqIndex = uint8(hBs.readBits(adtsLengthSamplingFrequencyIndex))
	bs.privateBit = uint8(hBs.readBits(adtsLengthPrivateBit))
	bs.channelConfig = uint8(hBs.readBits(adtsLengthChannelConfiguration))
	bs.original = uint8(hBs.readBits(adtsLengthOriginalCopy))
	bs.home = uint8(hBs.readBits(adtsLengthHome))

	// adts_variable_header
	bs.copyrightID = uint8(hBs.readBits(adtsLengthCopyrightIdentificationBit))
	bs.copyrightStart = uint8(hBs.readBits(adtsLengthCopyrightIdentificationStart))
	bs.frameLength = uint16(hBs.readBits(adtsLengthFrameLength))
	bs.adtsFullness = uint16(hBs.readBits(adtsLengthBufferFullness))
	bs.numRawBlocks = uint8(hBs.readBits(adtsLengthNumberOfRawDataBlocksInFrame))
	bs.numPceBits = 0

	headerLen := adtsHeaderLength

	if valBits < int(bs.frameLength)*8 {
		// bail
		hBs.pushBack(headerLen)
		return transportDecNotEnoughBits
	}

	// FDKcrcReset(&pAdts->crcInfo);
	crcReset(pAdts)
	if bs.protectionAbsent == 0 {
		// FDKpushBack(hBs, 56); crcStartReg; FDKpushFor(hBs, 56);
		hBs.pushBack(56)
		crcStartReg(pAdts, hBs, 0)
		hBs.pushFor(56)
	}

	if bs.protectionAbsent == 0 && bs.numRawBlocks > 0 {
		if hBs.getValidBits() < int(bs.numRawBlocks)*16 {
			// bail
			hBs.pushBack(headerLen)
			return transportDecNotEnoughBits
		}
		for i := 0; i < int(bs.numRawBlocks); i++ {
			pAdts.rawDataBlockDist[i] = uint16(hBs.readBits(16))
			headerLen += 16
		}
		// Change raw data blocks to delta values.
		pAdts.rawDataBlockDist[bs.numRawBlocks] =
			bs.frameLength - 7 - uint16(bs.numRawBlocks)*2 - 2
		for i := int(bs.numRawBlocks); i > 0; i-- {
			pAdts.rawDataBlockDist[i] -= pAdts.rawDataBlockDist[i-1]
		}
	}

	// adts_error_check
	if bs.protectionAbsent == 0 {
		crcEndReg(pAdts, hBs)

		if hBs.getValidBits() < adtsLengthCrcCheck {
			// bail
			hBs.pushBack(headerLen)
			return transportDecNotEnoughBits
		}

		crcCheckVal := uint16(hBs.readBits(adtsLengthCrcCheck))
		headerLen += adtsLengthCrcCheck

		setCrcReadValue(pAdts, crcCheckVal)
		// Check header CRC in case of multiple raw data blocks.
		if bs.numRawBlocks > 0 {
			if crcCheckVal != crcGetCRC(pAdts) {
				return transportDecCRCError
			}
			crcReset(pAdts)
		}
	}

	// check if valid header
	if bs.layer != 0 || // we only support MPEG ADTS
		bs.sampleFreqIndex >= 13 { // we only support 96kHz - 7350kHz
		hBs.pushFor(int(bs.frameLength) * 8) // try again one frame later
		return transportDecUnsupportedFormat
	}

	// special treatment of id-bit
	if bs.mpegID == 0 && pAdts.decoderCanDoMpeg4 == 0 {
		// MPEG-2 decoder cannot play MPEG-4 bitstreams.
		hBs.pushFor(int(bs.frameLength) * 8) // try again one frame later
		return transportDecUnsupportedFormat
	}

	if !ignoreBufferFullness {
		cmpBufferFullness := int(bs.frameLength)*8 +
			int(bs.adtsFullness)*32*getNumberOfEffectiveChannels(int(bs.channelConfig))

		// Evaluate buffer fullness.
		if bs.adtsFullness != 0x7FF {
			if pAdts.bufferFullnessStartFlag != 0 {
				if valBits < cmpBufferFullness {
					// Condition for start of decoding is not fulfilled; the
					// current frame will not be decoded.
					hBs.pushBack(headerLen)

					if (cmpBufferFullness + headerLen) > (((8192 * 4) << 3) - 7) {
						return transportDecSyncError
					}
					return transportDecNotEnoughBits
				}
				pAdts.bufferFullnessStartFlag = 0
			}
		}
	}

	// Get info from ADTS header — the AudioSpecificConfig fill and the
	// channel_config==0 PCE parse belong to the pce-asc area; this slice stops
	// at the integer field parse and hands the parsed `bs` to that area via
	// pAdts.bs below. See the seam note in this file's helpers.
	if bs.channelConfig == 0 {
		// The PCE branch (CProgramConfig_Read/Compare) is not part of the
		// frame-sync-parse slice; it is handled by the pce-asc area.
		return parsePCESeam()
	}

	// Copy bit stream data struct to persistent memory now, once we passed all
	// sanity checks above.
	pAdts.bs = bs

	return transportDecOK
}

// getRawDataBlockLength returns the length in bits of raw data block blockNum.
// 1:1 port of adtsRead_GetRawDataBlockLength in
// libfdk/libMpegTPDec/src/tpdec_adts.cpp:408. A return of -1 means the length is
// unknown.
func getRawDataBlockLength(pAdts *adts, blockNum int) int {
	var length int

	if pAdts.bs.numRawBlocks == 0 {
		// aac_frame_length subtracted by the header size (7 bytes).
		length = (int(pAdts.bs.frameLength) - 7) << 3
		if pAdts.bs.protectionAbsent == 0 {
			length -= 16 // subtract 16 bit CRC
		}
	} else {
		if pAdts.bs.protectionAbsent != 0 {
			length = -1 // raw data block length is unknown
		} else {
			if blockNum < 0 || blockNum > 3 {
				length = -1
			} else {
				length = (int(pAdts.rawDataBlockDist[blockNum]) << 3) - 16
			}
		}
	}
	if blockNum == 0 && length > 0 {
		length -= int(pAdts.bs.numPceBits)
	}
	return length
}

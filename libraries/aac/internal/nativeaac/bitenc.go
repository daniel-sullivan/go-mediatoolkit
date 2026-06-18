// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

// Bitstream-encode area: the AAC raw-data-block assembler.
//
// 1:1 port of bitenc.cpp — the bitstream encoder that turns a quantizer/coder
// output (QC_OUT) and psychoacoustic output (PSY_OUT) into AAC access-unit
// bits, or counts the static bits without emitting them. Every function names
// its bitenc.cpp C counterpart as file:line and translates the algorithm
// faithfully (do not "improve" it).
//
// Calling convention: the C code uses a NULL bit stream handle to mean
// "count static bits only, write nothing". The Go port preserves this — a nil
// *bitStream counts (BitStream.WriteBits short-circuits on a nil receiver) and
// a nil TransportEnc means hTpEnc == NULL.
//
// This whole area is integer-only; there is no aac_strict / default FP split.

package nativeaac

// File-local accounting offsets (bitenc.cpp:114).
const (
	globalGainOffset = 100
	icsReservedBit   = 0
	noiseOffset      = 90
)

// encodeSpectralData Huffman-encodes the spectral data of every section,
// returning the number of bits written (bitenc.cpp:127,
// FDKaacEnc_encodeSpectralData).
func encodeSpectralData(sfbOffset []int, sectionData *SectionData,
	quantSpectrum []int16, hBitStream *bitStream) int {
	dbgVal := int(hBitStream.getValidBitsWrite())

	for i := 0; i < sectionData.NoOfSections; i++ {
		if sectionData.HuffSection[i].CodeBook != codeBookPnsNo {
			// huffencode spectral data for this huffsection
			tmp := sectionData.HuffSection[i].SfbStart + sectionData.HuffSection[i].SfbCnt
			for sfb := sectionData.HuffSection[i].SfbStart; sfb < tmp; sfb++ {
				CodeValues(quantSpectrum[sfbOffset[sfb]:],
					sfbOffset[sfb+1]-sfbOffset[sfb],
					sectionData.HuffSection[i].CodeBook, hBitStream)
			}
		}
	}
	return int(hBitStream.getValidBitsWrite()) - dbgVal
}

// encodeGlobalGain encodes the common scale factor and returns the static bit
// count (bitenc.cpp:158, FDKaacEnc_encodeGlobalGain).
func encodeGlobalGain(globalGain, scalefac int, hBitStream *bitStream, mdctScale int) int {
	if hBitStream != nil {
		hBitStream.writeBits(
			uint32(globalGain-scalefac+globalGainOffset-4*(LogNormPCM-mdctScale)),
			8)
	}
	return 8
}

// encodeIcsInfo encodes the individual-channel-stream info and returns the
// static bit count (bitenc.cpp:180, FDKaacEnc_encodeIcsInfo).
func encodeIcsInfo(blockType, windowShape, groupingMask, maxSfbPerGroup int,
	hBitStream *bitStream, syntaxFlags uint32) int {
	var statBits int

	if blockType == ShortWindow {
		statBits = 8 + TransFac - 1
	} else {
		if syntaxFlags&ACELD != 0 {
			statBits = 6
		} else {
			if syntaxFlags&ACScalable == 0 {
				statBits = 11
			} else {
				statBits = 10
			}
		}
	}

	if hBitStream != nil {
		if syntaxFlags&ACELD == 0 {
			hBitStream.writeBits(icsReservedBit, 1)
			hBitStream.writeBits(uint32(blockType), 2)
			ws := windowShape
			if windowShape == LolWindow {
				ws = KbdWindow
			}
			hBitStream.writeBits(uint32(ws), 1)
		}

		switch blockType {
		case LongWindow, StartWindow, StopWindow:
			hBitStream.writeBits(uint32(maxSfbPerGroup), 6)

			if syntaxFlags&(ACScalable|ACELD) == 0 { // If not scalable syntax then ...
				// No predictor data present
				hBitStream.writeBits(0, 1)
			}

		case ShortWindow:
			hBitStream.writeBits(uint32(maxSfbPerGroup), 4)
			// Write grouping bits
			hBitStream.writeBits(uint32(groupingMask), TransFac-1)
		}
	}

	return statBits
}

// encodeSectionData encodes the section (codebook + length) data, returning
// the number of bits written (bitenc.cpp:239, FDKaacEnc_encodeSectionData).
func encodeSectionData(sectionData *SectionData, hBitStream *bitStream, useVCB11 uint32) int {
	if hBitStream != nil {
		var sectEscapeVal, sectLenBits int
		var sectLen int
		dbgVal := int(hBitStream.getValidBitsWrite())
		sectCbBits := 4

		switch sectionData.BlockType {
		case LongWindow, StartWindow, StopWindow:
			sectEscapeVal = SectEscValLong
			sectLenBits = SectBitsLong
		case ShortWindow:
			sectEscapeVal = SectEscValShort
			sectLenBits = SectBitsShort
		}

		for i := 0; i < sectionData.NoOfSections; i++ {
			codeBook := sectionData.HuffSection[i].CodeBook

			hBitStream.writeBits(uint32(codeBook), uint32(sectCbBits))

			sectLen = sectionData.HuffSection[i].SfbCnt

			for sectLen >= sectEscapeVal {
				hBitStream.writeBits(uint32(sectEscapeVal), uint32(sectLenBits))
				sectLen -= sectEscapeVal
			}
			hBitStream.writeBits(uint32(sectLen), uint32(sectLenBits))
		}
		return int(hBitStream.getValidBitsWrite()) - dbgVal
	}
	return 0
}

// encodeScaleFactorData encodes the DPCM-coded scalefactors, PNS energies and
// intensity scales, returning the bit count (or 1 on a coding-range error)
// (bitenc.cpp:292, FDKaacEnc_encodeScaleFactorData).
func encodeScaleFactorData(maxValueInSfb []uint, sectionData *SectionData,
	scalefac []int, hBitStream *bitStream, noiseNrg []int, isScale []int,
	globalGain int) int {
	if hBitStream != nil {
		var deltaScf int
		var deltaPns int
		lastValPns := 0
		noisePCMFlag := true
		var lastValIs int

		dbgVal := int(hBitStream.getValidBitsWrite())

		lastValScf := scalefac[sectionData.FirstScf]
		lastValPns = globalGain - scalefac[sectionData.FirstScf] +
			globalGainOffset - 4*LogNormPCM - noiseOffset
		lastValIs = 0

		for i := 0; i < sectionData.NoOfSections; i++ {
			if sectionData.HuffSection[i].CodeBook != codeBookZeroNo {
				if sectionData.HuffSection[i].CodeBook == codeBookIsOutOfPhaseNo ||
					sectionData.HuffSection[i].CodeBook == codeBookIsInPhaseNo {
					sfbStart := sectionData.HuffSection[i].SfbStart
					tmp := sfbStart + sectionData.HuffSection[i].SfbCnt
					for j := sfbStart; j < tmp; j++ {
						deltaIs := isScale[j] - lastValIs
						lastValIs = isScale[j]
						if CodeScalefactorDelta(deltaIs, hBitStream) != 0 {
							return 1
						}
					} // sfb
				} else if sectionData.HuffSection[i].CodeBook == codeBookPnsNo {
					sfbStart := sectionData.HuffSection[i].SfbStart
					tmp := sfbStart + sectionData.HuffSection[i].SfbCnt
					for j := sfbStart; j < tmp; j++ {
						deltaPns = noiseNrg[j] - lastValPns
						lastValPns = noiseNrg[j]

						if noisePCMFlag {
							hBitStream.writeBits(
								uint32(deltaPns+(1<<(PnsPCMBits-1))), PnsPCMBits)
							noisePCMFlag = false
						} else {
							if CodeScalefactorDelta(deltaPns, hBitStream) != 0 {
								return 1
							}
						}
					} // sfb
				} else {
					tmp := sectionData.HuffSection[i].SfbStart +
						sectionData.HuffSection[i].SfbCnt
					for j := sectionData.HuffSection[i].SfbStart; j < tmp; j++ {
						// check if we can repeat the last value to save bits
						if maxValueInSfb[j] == 0 {
							deltaScf = 0
						} else {
							deltaScf = -(scalefac[j] - lastValScf)
							lastValScf = scalefac[j]
						}
						if CodeScalefactorDelta(deltaScf, hBitStream) != 0 {
							return 1
						}
					} // sfb
				} // code scalefactor
			} // codeBook != CODE_BOOK_ZERO_NO
		} // section loop

		return int(hBitStream.getValidBitsWrite()) - dbgVal
	} // if hBitStream != NULL

	return 0
}

// encodeMSInfo encodes the MS-stereo info and returns the static bit count
// (bitenc.cpp:380, FDKaacEnc_encodeMSInfo).
func encodeMSInfo(sfbCnt, grpSfb, maxSfb, msDigest int, jsFlags []int,
	hBitStream *bitStream) int {
	msBits := 0

	if hBitStream != nil {
		switch msDigest {
		case MsNone:
			hBitStream.writeBits(SiMsMaskNone, 2)
			msBits += 2
		case MsAll:
			hBitStream.writeBits(SiMsMaskAll, 2)
			msBits += 2
		case MsSome:
			hBitStream.writeBits(SiMsMaskSome, 2)
			msBits += 2
			for sfbOff := 0; sfbOff < sfbCnt; sfbOff += grpSfb {
				for sfb := 0; sfb < maxSfb; sfb++ {
					if jsFlags[sfbOff+sfb]&MsOn != 0 {
						hBitStream.writeBits(1, 1)
					} else {
						hBitStream.writeBits(0, 1)
					}
					msBits++
				}
			}
		}
	} else {
		msBits += 2
		if msDigest == MsSome {
			for sfbOff := 0; sfbOff < sfbCnt; sfbOff += grpSfb {
				for sfb := 0; sfb < maxSfb; sfb++ {
					msBits++
				}
			}
		}
	}
	return msBits
}

// encodeTnsDataPresent writes the one-bit TNS-present flag and returns 1
// (bitenc.cpp:434, FDKaacEnc_encodeTnsDataPresent). tnsInfo is nil when the
// channel has no TNS info.
func encodeTnsDataPresent(tnsInfo *TnsInfo, blockType int, hBitStream *bitStream) int {
	if hBitStream != nil && tnsInfo != nil {
		tnsPresent := 0
		numOfWindows := 1
		if blockType == ShortWindow {
			numOfWindows = TransFac
		}

		for i := 0; i < numOfWindows; i++ {
			if tnsInfo.NumOfFilters[i] != 0 {
				tnsPresent = 1
				break
			}
		}

		if tnsPresent == 0 {
			hBitStream.writeBits(0, 1)
		} else {
			hBitStream.writeBits(1, 1)
		}
	}
	return 1
}

// encodeTnsData encodes the TNS filter orders and coefficients and returns the
// static bit count (bitenc.cpp:465, FDKaacEnc_encodeTnsData).
func encodeTnsData(tnsInfo *TnsInfo, blockType int, hBitStream *bitStream) int {
	tnsBits := 0

	if tnsInfo != nil {
		tnsPresent := 0
		var coefBits int
		numOfWindows := 1
		if blockType == ShortWindow {
			numOfWindows = TransFac
		}

		for i := 0; i < numOfWindows; i++ {
			if tnsInfo.NumOfFilters[i] != 0 {
				tnsPresent = 1
			}
		}

		if hBitStream != nil {
			if tnsPresent == 1 { // there is data to be written
				for i := 0; i < numOfWindows; i++ {
					nbf := 2
					if blockType == ShortWindow {
						nbf = 1
					}
					hBitStream.writeBits(uint32(tnsInfo.NumOfFilters[i]), uint32(nbf))
					tnsBits += nbf
					if tnsInfo.NumOfFilters[i] != 0 {
						cr := 0
						if tnsInfo.CoefRes[i] == 4 {
							cr = 1
						}
						hBitStream.writeBits(uint32(cr), 1)
						tnsBits++
					}
					for j := 0; j < tnsInfo.NumOfFilters[i]; j++ {
						lenBits := 6
						if blockType == ShortWindow {
							lenBits = 4
						}
						hBitStream.writeBits(uint32(tnsInfo.Length[i][j]), uint32(lenBits))
						tnsBits += lenBits
						ordBits := 5
						if blockType == ShortWindow {
							ordBits = 3
						}
						hBitStream.writeBits(uint32(tnsInfo.Order[i][j]), uint32(ordBits))
						tnsBits += ordBits
						if tnsInfo.Order[i][j] != 0 {
							hBitStream.writeBits(uint32(tnsInfo.Direction[i][j]), 1)
							tnsBits++ // direction
							if tnsInfo.CoefRes[i] == 4 {
								coefBits = 3
								for k := 0; k < tnsInfo.Order[i][j]; k++ {
									if tnsInfo.Coef[i][j][k] > 3 || tnsInfo.Coef[i][j][k] < -4 {
										coefBits = 4
										break
									}
								}
							} else {
								coefBits = 2
								for k := 0; k < tnsInfo.Order[i][j]; k++ {
									if tnsInfo.Coef[i][j][k] > 1 || tnsInfo.Coef[i][j][k] < -2 {
										coefBits = 3
										break
									}
								}
							}
							hBitStream.writeBits(uint32(-(coefBits-tnsInfo.CoefRes[i]))&1, 1) // coef_compres
							tnsBits++                                                         // coef_compression
							rmask := [...]int{0, 1, 3, 7, 15}
							for k := 0; k < tnsInfo.Order[i][j]; k++ {
								hBitStream.writeBits(
									uint32(int(tnsInfo.Coef[i][j][k])&rmask[coefBits]),
									uint32(coefBits))
								tnsBits += coefBits
							}
						}
					}
				}
			}
		} else {
			if tnsPresent != 0 {
				for i := 0; i < numOfWindows; i++ {
					if blockType == ShortWindow {
						tnsBits += 1
					} else {
						tnsBits += 2
					}
					if tnsInfo.NumOfFilters[i] != 0 {
						tnsBits++
						for j := 0; j < tnsInfo.NumOfFilters[i]; j++ {
							if blockType == ShortWindow {
								tnsBits += 4
							} else {
								tnsBits += 6
							}
							if blockType == ShortWindow {
								tnsBits += 3
							} else {
								tnsBits += 5
							}
							if tnsInfo.Order[i][j] != 0 {
								tnsBits++ // direction
								tnsBits++ // coef_compression
								if tnsInfo.CoefRes[i] == 4 {
									coefBits = 3
									for k := 0; k < tnsInfo.Order[i][j]; k++ {
										if tnsInfo.Coef[i][j][k] > 3 || tnsInfo.Coef[i][j][k] < -4 {
											coefBits = 4
											break
										}
									}
								} else {
									coefBits = 2
									for k := 0; k < tnsInfo.Order[i][j]; k++ {
										if tnsInfo.Coef[i][j][k] > 1 || tnsInfo.Coef[i][j][k] < -2 {
											coefBits = 3
											break
										}
									}
								}
								for k := 0; k < tnsInfo.Order[i][j]; k++ {
									tnsBits += coefBits
								}
							}
						}
					}
				}
			}
		}
	} // tnsInfo != NULL

	return tnsBits
}

// encodeGainControlData writes the (unsupported) gain-control present flag and
// returns 1 (bitenc.cpp:589, FDKaacEnc_encodeGainControlData).
func encodeGainControlData(hBitStream *bitStream) int {
	if hBitStream != nil {
		hBitStream.writeBits(0, 1)
	}
	return 1
}

// encodePulseData writes the (unsupported) pulse-data present flag and returns
// 1 (bitenc.cpp:605, FDKaacEnc_encodePulseData).
func encodePulseData(hBitStream *bitStream) int {
	if hBitStream != nil {
		hBitStream.writeBits(0, 1)
	}
	return 1
}

// Extension-payload field widths (bitenc.cpp:625).
const (
	extTypeBits       = 4
	dataElVersionBits = 4
	fillNibbleBits    = 4
)

// writeExtensionPayload writes an extension payload (or counts its bits) and
// returns the bits used (bitenc.cpp:621, FDKaacEnc_writeExtensionPayload).
func writeExtensionPayload(hBitStream *bitStream, extPayloadType int,
	extPayloadData []byte, extPayloadBits int) int {
	extBitsUsed := 0

	if extPayloadBits >= extTypeBits {
		fillByte := byte(0x00) // for EXT_FIL and EXT_FILL_DATA
		dp := 0                // running index into extPayloadData

		if hBitStream != nil {
			hBitStream.writeBits(uint32(extPayloadType), extTypeBits)
		}
		extBitsUsed += extTypeBits

		switch extPayloadType {
		case ExtLdsacData:
			if hBitStream != nil {
				hBitStream.writeBits(uint32(extPayloadData[dp]), 4) // nibble
				dp++
			}
			extBitsUsed += 4
			fallthrough
		case ExtDynamicRange, ExtSbrData, ExtSbrDataCRC:
			if hBitStream != nil {
				writeBits := extPayloadBits
				for ; writeBits >= 8; dp++ {
					hBitStream.writeBits(uint32(extPayloadData[dp]), 8)
					writeBits -= 8
				}
				if writeBits > 0 {
					hBitStream.writeBits(uint32(extPayloadData[dp])>>(8-uint(writeBits)),
						uint32(writeBits))
				}
			}
			extBitsUsed += extPayloadBits

		case ExtDataElement:
			dataElementLength := (extPayloadBits + 7) >> 3
			cnt := dataElementLength
			loopCounter := 1

			for dataElementLength >= 255 {
				loopCounter++
				dataElementLength -= 255
			}

			if hBitStream != nil {
				hBitStream.writeBits(0x00, dataElVersionBits) // data_element_version = ANC_DATA

				for i := 1; i < loopCounter; i++ {
					hBitStream.writeBits(255, 8)
				}
				hBitStream.writeBits(uint32(dataElementLength), 8)

				for i := 0; i < cnt; i++ {
					hBitStream.writeBits(uint32(extPayloadData[i]), 8)
				}
			}
			extBitsUsed += dataElVersionBits + (loopCounter * 8) + (cnt * 8)

		case ExtFillData:
			fillByte = 0xA5
			fallthrough
		default: // EXT_FIL and any other
			if hBitStream != nil {
				writeBits := extPayloadBits
				hBitStream.writeBits(0x00, fillNibbleBits)
				writeBits -= 8 // account for the extension type and the fill nibble
				for writeBits >= 8 {
					hBitStream.writeBits(uint32(fillByte), 8)
					writeBits -= 8
				}
			}
			extBitsUsed += fillNibbleBits + (extPayloadBits &^ 0x7) - 8
		}
	}

	return extBitsUsed
}

// Data-stream-element field widths and limits (bitenc.cpp:729).
const (
	dataByteAlignFlag     = 0
	elInstanceTagBits     = 4
	dataByteAlignFlagBits = 1
	dataLenCountBits      = 8
	dataLenEscCountBits   = 8
	maxDataAlignBits      = 7
	maxDseDataBytes       = 510
)

// writeDataStreamElement writes a data stream element (e.g. ancillary data),
// returning the bits used (bitenc.cpp:724, FDKaacEnc_writeDataStreamElement).
func writeDataStreamElement(hTpEnc TransportEnc, elementInstanceTag,
	dataPayloadBytes int, dataBuffer []byte, alignAnchor uint32) int {
	dseBitsUsed := 0

	for dataPayloadBytes > 0 {
		escCount := -1
		cnt := 0
		crcReg := -1

		dseBitsUsed += ElIDBits + elInstanceTagBits +
			dataByteAlignFlagBits + dataLenCountBits

		if dataByteAlignFlag != 0 {
			dseBitsUsed += maxDataAlignBits
		}

		cnt = fixMin(maxDseDataBytes, dataPayloadBytes)
		if cnt >= 255 {
			escCount = cnt - 255
			dseBitsUsed += dataLenEscCountBits
		}

		dataPayloadBytes -= cnt
		dseBitsUsed += cnt * 8

		if hTpEnc != nil {
			hBitStream := hTpEnc.GetBitstream()

			hBitStream.writeBits(IDDSE, ElIDBits)

			crcReg = hTpEnc.CrcStartReg(0)

			hBitStream.writeBits(uint32(elementInstanceTag), elInstanceTagBits)
			hBitStream.writeBits(dataByteAlignFlag, dataByteAlignFlagBits)

			// write length field(s)
			if escCount >= 0 {
				hBitStream.writeBits(255, dataLenCountBits)
				hBitStream.writeBits(uint32(escCount), dataLenEscCountBits)
			} else {
				hBitStream.writeBits(uint32(cnt), dataLenCountBits)
			}

			if dataByteAlignFlag != 0 {
				tmp := int(hBitStream.getValidBitsWrite())
				hBitStream.byteAlignWrite(alignAnchor)
				// count actual bits
				dseBitsUsed += int(hBitStream.getValidBitsWrite()) - tmp - maxDataAlignBits
			}

			// write payload
			for i := 0; i < cnt; i++ {
				hBitStream.writeBits(uint32(dataBuffer[i]), 8)
			}
			hTpEnc.CrcEndReg(crcReg)
		}
	}

	return dseBitsUsed
}

// Fill-element field widths and limit (bitenc.cpp:815).
const (
	fillElCountBits    = 4
	fillElEscCountBits = 8
	maxFillDataBytes   = 269
)

// WriteExtensionData writes (or accounts) one extension payload, packing it
// into fill elements / DSEs for GA bitstreams or en-bloc for ER / scalable
// (bitenc.cpp:809, FDKaacEnc_writeExtensionData).
func WriteExtensionData(hTpEnc TransportEnc, pExtension *QcOutExtension,
	elInstanceTag int, alignAnchor uint32, syntaxFlags uint32, aot int,
	epConfig int8) int {
	var hBitStream *bitStream
	payloadBits := pExtension.NPayloadBits
	extBitsUsed := 0

	if hTpEnc != nil {
		hBitStream = hTpEnc.GetBitstream()
	}

	if syntaxFlags&(ACScalable|ACER) != 0 {
		if (syntaxFlags&ACELD != 0) &&
			((pExtension.Type == ExtSbrData) || (pExtension.Type == ExtSbrDataCRC)) {
			if hBitStream != nil {
				writeBits := payloadBits
				extPayloadData := pExtension.Payload
				i := 0
				for ; writeBits >= 8; i++ {
					hBitStream.writeBits(uint32(extPayloadData[i]), 8)
					writeBits -= 8
				}
				if writeBits > 0 {
					hBitStream.writeBits(uint32(extPayloadData[i])>>(8-uint(writeBits)),
						uint32(writeBits))
				}
			}
			extBitsUsed += payloadBits
		} else {
			// ER or scalable syntax -> write extension en bloc
			extBitsUsed += writeExtensionPayload(
				hBitStream, pExtension.Type, pExtension.Payload, payloadBits)
		}
	} else {
		// We have normal GA bitstream payload (AOT 2,5,29) so pack the data
		// into fill elements or DSEs

		if pExtension.Type == ExtDataElement {
			extBitsUsed += writeDataStreamElement(
				hTpEnc, elInstanceTag, pExtension.NPayloadBits>>3,
				pExtension.Payload, alignAnchor)
		} else {
			for payloadBits >= (ElIDBits + fillElCountBits) {
				escCount := -1
				alignBits := 7

				if (pExtension.Type == ExtFillData) || (pExtension.Type == ExtFIL) {
					payloadBits -= ElIDBits + fillElCountBits
					if payloadBits >= 15*8 {
						payloadBits -= fillElEscCountBits
						escCount = 0 // write esc_count even if cnt becomes smaller 15
					}
					alignBits = 0
				}

				cnt := fixMin(maxFillDataBytes, (payloadBits+alignBits)>>3)

				if cnt >= 15 {
					escCount = cnt - 15 + 1
				}

				if hBitStream != nil {
					// write bitstream
					hBitStream.writeBits(IDFIL, ElIDBits)
					if escCount >= 0 {
						hBitStream.writeBits(15, fillElCountBits)
						hBitStream.writeBits(uint32(escCount), fillElEscCountBits)
					} else {
						hBitStream.writeBits(uint32(cnt), fillElCountBits)
					}
				}

				extBitsUsed += ElIDBits + fillElCountBits
				if escCount >= 0 {
					extBitsUsed += fillElEscCountBits
				}

				cnt = fixMin(cnt*8, payloadBits) // convert back to bits
				extBitsUsed += writeExtensionPayload(
					hBitStream, pExtension.Type, pExtension.Payload, cnt)
				payloadBits -= cnt
			}
		}
	}

	return extBitsUsed
}

// byteAlignment writes alignBits zero bits (bitenc.cpp:913,
// FDKaacEnc_ByteAlignment).
func byteAlignment(hBitStream *bitStream, alignBits int) {
	hBitStream.writeBits(0, uint32(alignBits))
}

// ChannelElementWrite assembles (or accounts the static bits of) one channel
// element by walking the element list returned by getBitstreamElementList
// (bitenc.cpp:918, FDKaacEnc_ChannelElementWrite). The static-bit count is
// returned via *pBitDemand. With hTpEnc nil and minCnt non-zero this computes
// the minimum static bits with all tools disabled.
func ChannelElementWrite(hTpEnc TransportEnc, pElInfo *ElementInfo,
	qcOutChannel []*QcOutChannel, psyOutElement *PsyOutElement,
	psyOutChannel []*PsyOutChannel, syntaxFlags uint32, aot int, epConfig int8,
	pBitDemand *int, minCnt uint8) EncoderError {
	error := AacEncOK
	var hBitStream *bitStream
	bitDemand := 0
	var list *elementList
	var i, ch, decisionBit int
	crcReg1, crcReg2 := -1, -1
	var numberOfChannels uint8

	if hTpEnc != nil {
		// Get bitstream handle
		hBitStream = hTpEnc.GetBitstream()
	}

	if (pElInfo.ElType == IDSCE) || (pElInfo.ElType == IDLFE) {
		numberOfChannels = 1
	} else {
		numberOfChannels = 2
	}

	// Get channel element sequence table
	list = getBitstreamElementList(aot, int(epConfig), int(numberOfChannels), 0, 0)
	if list == nil {
		error = AacEncUnsupportedAOT
		goto bail
	}

	if syntaxFlags&(ACScalable|ACER) == 0 {
		if hBitStream != nil {
			hBitStream.writeBits(uint32(pElInfo.ElType), ElIDBits)
		}
		bitDemand += ElIDBits
	}

	// Iterate through sequence table
	i = 0
	ch = 0
	decisionBit = 0
	for {
		// some tmp values
		var pChSectionData *SectionData
		var pChScf []int
		var pChMaxValueInSfb []uint
		var pTnsInfo *TnsInfo
		chGlobalGain := 0
		chBlockType := 0
		chMaxSfbPerGrp := 0
		chSfbPerGrp := 0
		chSfbCnt := 0
		chFirstScf := 0

		if minCnt == 0 {
			if qcOutChannel != nil {
				pChSectionData = &qcOutChannel[ch].SectionData
				pChScf = qcOutChannel[ch].Scf[:]
				chGlobalGain = qcOutChannel[ch].GlobalGain
				pChMaxValueInSfb = qcOutChannel[ch].MaxValueInSfb[:]
				chBlockType = pChSectionData.BlockType
				chMaxSfbPerGrp = pChSectionData.MaxSfbPerGroup
				chSfbPerGrp = pChSectionData.SfbPerGroup
				chSfbCnt = pChSectionData.SfbCnt
				chFirstScf = pChScf[pChSectionData.FirstScf]
			} else {
				// get values from PSY
				chSfbCnt = psyOutChannel[ch].SfbCnt
				chSfbPerGrp = psyOutChannel[ch].SfbPerGroup
				chMaxSfbPerGrp = psyOutChannel[ch].MaxSfbPerGroup
			}
			pTnsInfo = &psyOutChannel[ch].TnsInfo
		} // minCnt==0

		if qcOutChannel == nil {
			chBlockType = psyOutChannel[ch].LastWindowSequence
		}

		switch list.id[i] {
		case elementInstanceTag:
			// Write element instance tag
			if hBitStream != nil {
				hBitStream.writeBits(uint32(pElInfo.InstanceTag), 4)
			}
			bitDemand += 4

		case commonWindow:
			// Write common window flag
			decisionBit = psyOutElement.CommonWindow
			if hBitStream != nil {
				hBitStream.writeBits(uint32(psyOutElement.CommonWindow), 1)
			}
			bitDemand += 1

		case icsInfo:
			// Write individual channel info
			bitDemand += encodeIcsInfo(chBlockType, psyOutChannel[ch].WindowShape,
				psyOutChannel[ch].GroupingMask, chMaxSfbPerGrp, hBitStream, syntaxFlags)

		case ltpDataPresent:
			// Write LTP data present flag
			if hBitStream != nil {
				hBitStream.writeBits(0, 1)
			}
			bitDemand += 1

		case ltpData:
			// Predictor data not supported. Nothing to do here.

		case ms:
			// Write MS info
			md := MsNone
			var msMask []int
			if minCnt == 0 {
				md = psyOutElement.ToolsInfo.MsDigest
			}
			msMask = psyOutElement.ToolsInfo.MsMask[:]
			bitDemand += encodeMSInfo(chSfbCnt, chSfbPerGrp, chMaxSfbPerGrp,
				md, msMask, hBitStream)

		case globalGain:
			bitDemand += encodeGlobalGain(chGlobalGain, chFirstScf, hBitStream,
				psyOutChannel[ch].MdctScale)

		case sectionData:
			siBits := encodeSectionData(pChSectionData, hBitStream,
				boolToUint(syntaxFlags&ACERVCB11 != 0))
			if hBitStream != nil {
				if siBits != qcOutChannel[ch].SectionData.SideInfoBits {
					error = AacEncWriteSecError
				}
			}
			bitDemand += siBits

		case scaleFactorData:
			sfDataBits := encodeScaleFactorData(pChMaxValueInSfb, pChSectionData,
				pChScf, hBitStream, psyOutChannel[ch].NoiseNrg[:],
				psyOutChannel[ch].IsScale[:], chGlobalGain)
			if (hBitStream != nil) &&
				(sfDataBits != (qcOutChannel[ch].SectionData.ScalefacBits +
					qcOutChannel[ch].SectionData.NoiseNrgBits)) {
				error = AacEncWriteScalError
			}
			bitDemand += sfDataBits

		case esc2RVLC:
			if syntaxFlags&ACERRVLC != 0 {
				// write RVLC data into bitstream (error sens. cat. 2)
				error = AacEncUnsupportedAOT
			}

		case pulse:
			// Write pulse data
			bitDemand += encodePulseData(hBitStream)

		case tnsDataPresent:
			// Write TNS data present flag
			bitDemand += encodeTnsDataPresent(pTnsInfo, chBlockType, hBitStream)

		case tnsData:
			// Write TNS data
			bitDemand += encodeTnsData(pTnsInfo, chBlockType, hBitStream)

		case gainControlData:
			// Nothing to do here

		case gainControlDataPresent:
			bitDemand += encodeGainControlData(hBitStream)

		case esc1HCR:
			if syntaxFlags&ACERHCR != 0 {
				error = AacEncUnknown
			}

		case spectralData:
			if hBitStream != nil {
				spectralBits := encodeSpectralData(psyOutChannel[ch].SfbOffsets[:],
					pChSectionData, qcOutChannel[ch].QuantSpec[:], hBitStream)

				if spectralBits != qcOutChannel[ch].SectionData.HuffmanBits {
					return AacEncWriteSpecError
				}
				bitDemand += spectralBits
			}

		// Non data cases
		case adtscrcStartReg1:
			if hTpEnc != nil {
				crcReg1 = hTpEnc.CrcStartReg(192)
			}
		case adtscrcStartReg2:
			if hTpEnc != nil {
				crcReg2 = hTpEnc.CrcStartReg(128)
			}
		case adtscrcEndReg1, drmcrcEndReg:
			if hTpEnc != nil {
				hTpEnc.CrcEndReg(crcReg1)
			}
		case adtscrcEndReg2:
			if hTpEnc != nil {
				hTpEnc.CrcEndReg(crcReg2)
			}
		case drmcrcStartReg:
			if hTpEnc != nil {
				crcReg1 = hTpEnc.CrcStartReg(0)
			}
		case nextChannel:
			ch = (ch + 1) % int(numberOfChannels)
		case linkSequence:
			list = list.next[decisionBit]
			i = -1

		default:
			error = AacEncUnknown
		}

		if error != AacEncOK {
			return error
		}

		i++

		if list.id[i] == endOfSequence {
			break
		}
	}

bail:
	if pBitDemand != nil {
		*pBitDemand = bitDemand
	}

	return error
}

// boolToUint maps a Go bool to the UINT 0/1 the C passes for useVCB11.
func boolToUint(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// WriteBitstream assembles a full AAC access unit from the channel mapping,
// quantizer/coder output and psychoacoustic output, byte-aligns it and ends
// the access unit (bitenc.cpp:1182, FDKaacEnc_WriteBitstream).
func WriteBitstream(hTpEnc TransportEnc, channelMapping *ChannelMapping,
	qcOut *QcOut, psyOut *PsyOut, qcKernel *QcState, aot int, syntaxFlags uint32,
	epConfig int8) EncoderError {
	hBs := hTpEnc.GetBitstream()
	errorStatus := AacEncOK
	doByteAlign := 1
	var bitMarkUp int
	var frameBits int
	// Get first bit of raw data block. In case of ADTS+PCE, AU would start at
	// PCE. This is okay because PCE assures alignment.
	alignAnchor := hBs.getValidBitsWrite()

	frameBits = int(alignAnchor)
	bitMarkUp = int(alignAnchor)

	// Channel element loop
	for i := 0; i < channelMapping.NElements; i++ {
		elInfo := channelMapping.ElInfo[i]
		elementUsedBits := 0

		switch elInfo.ElType {
		case IDSCE, IDCPE, IDLFE: // single / pair / LFE channel
			if errorStatus = ChannelElementWrite(
				hTpEnc, &elInfo, qcOut.QcElement[i].QcOutChannel[:],
				psyOut.PsyOutElement[i], psyOut.PsyOutElement[i].PsyOutChannel[:],
				syntaxFlags, aot, epConfig, nil, 0); errorStatus != AacEncOK {
				return errorStatus
			}

			if syntaxFlags&ACER == 0 {
				// Write associated extension payload
				for n := 0; n < qcOut.QcElement[i].NExtensions; n++ {
					WriteExtensionData(hTpEnc, &qcOut.QcElement[i].Extension[n], 0,
						alignAnchor, syntaxFlags, aot, epConfig)
				}
			}

		// In FDK, DSE signalling explicit done in elDSE. See channel_map.cpp
		default:
			return AacEncInvalidElementInfoType
		} // switch

		if elInfo.ElType != IDDSE {
			elementUsedBits -= bitMarkUp
			bitMarkUp = int(hBs.getValidBitsWrite())
			elementUsedBits += bitMarkUp
			frameBits += elementUsedBits
		}
	} // for nElements

	if (syntaxFlags&ACER != 0) && (syntaxFlags&ACDRM == 0) {
		// 0: extension not touched, 1: extension already written
		var channelElementExtensionWritten [8][1]uint8

		if syntaxFlags&ACELD != 0 {
			for i := 0; i < channelMapping.NElements; i++ {
				for n := 0; n < qcOut.QcElement[i].NExtensions; n++ {
					if (qcOut.QcElement[i].Extension[n].Type == ExtSbrData) ||
						(qcOut.QcElement[i].Extension[n].Type == ExtSbrDataCRC) {
						// Write sbr extension payload
						WriteExtensionData(hTpEnc, &qcOut.QcElement[i].Extension[n], 0,
							alignAnchor, syntaxFlags, aot, epConfig)

						channelElementExtensionWritten[i][n] = 1
					} // SBR
				} // n
			} // i
		} // AC_ELD

		for i := 0; i < channelMapping.NElements; i++ {
			for n := 0; n < qcOut.QcElement[i].NExtensions; n++ {
				if channelElementExtensionWritten[i][n] == 0 {
					// Write all remaining extension payloads in element
					WriteExtensionData(hTpEnc, &qcOut.QcElement[i].Extension[n], 0,
						alignAnchor, syntaxFlags, aot, epConfig)
				}
			} // n
		} // i
	} // if AC_ER

	// Extend global extension payload table with fill bits
	n := qcOut.NExtensions

	// Add fill data / stuffing bits
	qcOut.Extension[n].Type = ExtFillData
	qcOut.Extension[n].NPayloadBits = qcOut.TotFillBits
	qcOut.NExtensions++

	// Write global extension payload and fill data
	for n := 0; (n < qcOut.NExtensions) && (n < (2 + 2)); n++ {
		WriteExtensionData(hTpEnc, &qcOut.Extension[n], 0, alignAnchor,
			syntaxFlags, aot, epConfig)
		// For EXT_FIL or EXT_FILL_DATA we could do an additional sanity check here
	}

	if syntaxFlags&(ACScalable|ACER) == 0 {
		hBs.writeBits(IDEND, ElIDBits)
	}

	if doByteAlign != 0 {
		// Assure byte alignment
		if ((int(hBs.getValidBitsWrite()) - int(alignAnchor) + qcOut.AlignBits) & 0x7) != 0 {
			return AacEncWrittenBitsError
		}

		byteAlignment(hBs, qcOut.AlignBits)
	}

	frameBits -= bitMarkUp
	frameBits += int(hBs.getValidBitsWrite())

	hTpEnc.EndAccessUnit(&frameBits)

	if frameBits != qcOut.TotalBits+qcKernel.GlobHdrBits {
		return AacEncWrittenBitsError
	}

	return errorStatus
}

// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// This file ports the plain spectral Huffman decode area of the vendored
// Fraunhofer FDK-AAC reference 1:1: the two Huffman-word tree walkers and the
// escape-sequence reader from libAACdec/src/block.h and block.cpp, plus the
// non-HCR ("plain huffman decoder") path of CBlock_ReadSpectralData
// (block.cpp:620). The HCR (Huffman Codeword Reordering, AC_ER_HCR) branch is
// a separate area and is not ported here.
//
// These are integer kernels: the unpacked quantized MDCT coefficients are
// bit-identical regardless of build tag. The decode reads from the cache-level
// bit reader in bitstream.go, matching the C bit-consumption order exactly.
//
// The C reads its inputs out of CAacDecoderChannelInfo / icsInfo; to keep this
// area self-contained (interface-first, per the port discipline) the loop
// takes a spectralInput that mirrors precisely the fields block.cpp:620 reads:
// the per-band codebook array, the scalefactor-band offsets, the window-group
// structure, the granule length, and the transmitted band count. Output is the
// flat MDCT spectrum (FIXP_DBL == int32 in the fixed-point reference).

// decodeHuffmanWord walks a codebook decode tree two bits at a time.
//
// C counterpart: CBlock_DecodeHuffmanWord (libAACdec/src/block.h:300).
func decodeHuffmanWord(bs *bitStream, codeBook [][huffmanEntries]uint16) int {
	var val uint32
	var index uint32

	for {
		val = uint32(codeBook[index][bs.readBits(huffmanBits)]) // Expensive memory access

		if (val & 1) == 0 {
			index = val >> 2
			continue
		}
		if val&2 != 0 {
			bs.pushBackCache(1)
		}
		val >>= 2
		break
	}

	return int(val)
}

// decodeHuffmanWordCB walks a codebook decode tree two bits at a time — the
// optimised variant taking the codebook directly and using read2Bits.
//
// C counterpart: CBlock_DecodeHuffmanWordCB (libAACdec/src/block.h:327).
func decodeHuffmanWordCB(bs *bitStream, codeBook [][huffmanEntries]uint16) int {
	var index uint32

	for {
		index = uint32(codeBook[index][bs.read2Bits()]) // Expensive memory access
		if index&1 != 0 {
			break
		}
		index >>= 2
	}
	if index&2 != 0 {
		bs.pushBackCache(1)
	}
	return int(index >> 2)
}

// getEscape reads the escape sequence of a codeword when the absolute value of
// the quantized coefficient is 16. The prefix is limited to a maximum of eight
// 1's (21 bits total) per ISO/IEC 14496-3:2009(E) 4.6.3.3; an overlong prefix
// returns MAX_QUANTIZED_VALUE+1 regardless of sign.
//
// C counterpart: CBlock_GetEscape (libAACdec/src/block.cpp:138).
func getEscape(bs *bitStream, q int) int {
	if iabs(q) != 16 {
		return q
	}

	var i, off int
	for i = 4; i < 13; i++ {
		if bs.readBit() == 0 {
			break
		}
	}

	if i == 13 {
		return maxQuantizedValue + 1
	}

	off = int(bs.readBits(uint32(i)))
	i = off + (1 << uint(i))

	if q < 0 {
		i = -i
	}

	return i
}

// iabs ports the FDK fAbs for the LONG (int32-range) values getEscape handles.
//
// C counterpart: fAbs as used by CBlock_GetEscape (libAACdec/src/block.cpp:141).
func iabs(q int) int {
	if q < 0 {
		return -q
	}
	return q
}

// spectralInput mirrors the fields of CAacDecoderChannelInfo / icsInfo that
// the plain Huffman path of CBlock_ReadSpectralData (libAACdec/src/block.cpp:620)
// reads. codeBook is the flat per-(group*16+band) codebook array
// (pDynData->aCodeBook); bandOffsets is the scalefactor-band offset table
// (GetScaleFactorBandOffsets), indexed [0 .. transmittedBands]; windowGroups
// and windowGroupLength describe the group structure (GetWindowGroups /
// GetWindowGroupLength); granuleLength is the per-window spectrum stride; and
// transmittedBands is GetScaleFactorBandsTransmitted.
type spectralInput struct {
	codeBook         []byte
	bandOffsets      []int16
	windowGroups     int
	windowGroupLen   []int
	granuleLength    int
	transmittedBands int
}

// readSpectralData ports the non-HCR plain-Huffman branch of
// CBlock_ReadSpectralData (libAACdec/src/block.cpp:620): it clears the
// spectrum, then for every transmitted scalefactor band of every window group
// unpacks the quantized MDCT coefficients with decodeHuffmanWordCB, applying
// the codebook offset, sign bits, and ESCBOOK escapes exactly as the C.
//
// The spectrum slice is the flat MDCT coefficient buffer
// (pSpectralCoefficient), laid out window-major with stride granuleLength; the
// caller sizes it (the C clears sizeof(SPECTRUM)). Note the C patches input
// codebooks 16..31 (VCB11) down to 11 in place, mutating aCodeBook — the same
// in-place write happens here on in.codeBook.
func readSpectralData(bs *bitStream, in *spectralInput, spectrum []int32) {
	var index, i int

	bandOffsets := in.bandOffsets

	for k := range spectrum {
		spectrum[k] = 0
	}

	groupoffset := 0

	// plain huffman decoder  short
	maxGroup := in.windowGroups

	for group := 0; group < maxGroup; group++ {
		maxGroupwin := in.windowGroupLen[group]

		bnds := group * 16

		bandOffset1 := int(bandOffsets[0])
		for band := 0; band < in.transmittedBands; band, bnds = band+1, bnds+1 {
			currentCB := in.codeBook[bnds]
			bandOffset0 := bandOffset1
			bandOffset1 = int(bandOffsets[band+1])

			// patch to run plain-huffman-decoder with vcb11 input codebooks
			// (LAV-checking might be possible below using the virtual cb and a
			// LAV-table)
			if currentCB >= 16 && currentCB <= 31 {
				currentCB = 11
				in.codeBook[bnds] = currentCB
			}
			if currentCB != zeroHCB && currentCB != noiseHCB &&
				currentCB != intensityHCB && currentCB != intensityHCB2 {
				hcb := &aacCodeBookDescriptionTable[currentCB]
				step := hcb.dimension
				offset := hcb.offset
				bits := hcb.numBits
				mask := (1 << uint(bits)) - 1
				codeBook := hcb.codeBook

				mdctBase := groupoffset * in.granuleLength

				if offset == 0 {
					for groupwin := 0; groupwin < maxGroupwin; groupwin++ {
						for index = bandOffset0; index < bandOffset1; index += step {
							idx := decodeHuffmanWordCB(bs, codeBook)
							for i = 0; i < step; i, idx = i+1, idx>>uint(bits) {
								tmp := int32((idx & mask) - offset)
								if tmp != 0 {
									if bs.readBit() != 0 {
										tmp = -tmp
									}
								}
								spectrum[mdctBase+index+i] = tmp
							}

							if currentCB == escBook {
								for j := 0; j < 2; j++ {
									spectrum[mdctBase+index+j] = int32(getEscape(
										bs, int(spectrum[mdctBase+index+j])))
								}
							}
						}
						mdctBase += in.granuleLength
					}
				} else {
					for groupwin := 0; groupwin < maxGroupwin; groupwin++ {
						for index = bandOffset0; index < bandOffset1; index += step {
							idx := decodeHuffmanWordCB(bs, codeBook)
							for i = 0; i < step; i, idx = i+1, idx>>uint(bits) {
								spectrum[mdctBase+index+i] = int32((idx & mask) - offset)
							}
							if currentCB == escBook {
								for j := 0; j < 2; j++ {
									spectrum[mdctBase+index+j] = int32(getEscape(
										bs, int(spectrum[mdctBase+index+j])))
								}
							}
						}
						mdctBase += in.granuleLength
					}
				}
			}
		}
		groupoffset += maxGroupwin
	}
	// plain huffman decoding (short) finished
}

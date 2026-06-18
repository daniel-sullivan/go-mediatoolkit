// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

// This file exposes the raw_data_block syntax serializers (bitenc.go's
// encodeIcsInfo / encodeSectionData / encodeScaleFactorData / encodeMSInfo /
// encodeTnsDataPresent / encodeTnsData / encodeSpectralData) to the
// enc-bitstream parity oracle, which lives in a separate package
// (internal/parity_tests/enc-bitstream) and so cannot reach the unexported
// bitStream type, newWriteBitStream, or the section-level static functions
// across the package boundary. Mirrors the CodeValuesParity seam in
// bitenc_parity_export.go and the EncMsStereoProcessing seam used by
// enc-stereo-tns. Not part of the shipping surface — purely a test seam.
//
// Each wrapper drives the writer end to end (the section serializer ->
// writeBits -> fdkPut ring store -> getValidBitsWrite -> byteAlignWrite) so the
// comparison covers the exact code path the raw-data-block assembler
// (bitenc.go ChannelElementWrite) takes for that element, and returns the
// produced byte buffer plus the static-bit count the function reports.

package nativeaac

// EncodeIcsInfoParity serializes the individual-channel-stream info into a
// fresh bufBytes-byte buffer, byte-aligns and flushes, and returns the produced
// bytes plus the static-bit count encodeIcsInfo reports (bitenc.cpp:180,
// FDKaacEnc_encodeIcsInfo). Exported for the enc-bitstream parity oracle.
func EncodeIcsInfoParity(blockType, windowShape, groupingMask, maxSfbPerGroup,
	bufBytes int, syntaxFlags uint32) (out []byte, statBits int) {
	buf := make([]byte, bufBytes)
	bs := newWriteBitStream(buf)
	statBits = encodeIcsInfo(blockType, windowShape, groupingMask,
		maxSfbPerGroup, bs, syntaxFlags)
	bs.byteAlignWrite(0)
	nBytes := (int(bs.getValidBitsWrite()) + 7) >> 3
	return buf[:nBytes], statBits
}

// EncodeGlobalGainParity serializes the global gain (common scale factor) and
// returns the produced bytes plus the static-bit count (bitenc.cpp:158,
// FDKaacEnc_encodeGlobalGain). Exported for the enc-bitstream parity oracle.
func EncodeGlobalGainParity(globalGain, scalefac, mdctScale, bufBytes int) (out []byte, statBits int) {
	buf := make([]byte, bufBytes)
	bs := newWriteBitStream(buf)
	statBits = encodeGlobalGain(globalGain, scalefac, bs, mdctScale)
	bs.byteAlignWrite(0)
	nBytes := (int(bs.getValidBitsWrite()) + 7) >> 3
	return buf[:nBytes], statBits
}

// SectionDesc describes one Huffman section for the section / scalefactor /
// spectral parity wrappers. It mirrors the fields of SectionInfo the
// raw_data_block serializers read.
type SectionDesc struct {
	CodeBook int
	SfbStart int
	SfbCnt   int
}

// buildSectionData assembles a SectionData from a block type and a section list
// for the parity wrappers. firstScf is the first scalefactor band to be coded.
func buildSectionData(blockType, firstScf int, sections []SectionDesc) *SectionData {
	sd := new(SectionData)
	sd.BlockType = blockType
	sd.NoOfSections = len(sections)
	sd.FirstScf = firstScf
	for i, s := range sections {
		sd.HuffSection[i] = SectionInfo{
			CodeBook: s.CodeBook,
			SfbStart: s.SfbStart,
			SfbCnt:   s.SfbCnt,
		}
	}
	return sd
}

// EncodeSectionDataParity serializes the section (codebook + length) data into a
// fresh bufBytes-byte buffer, byte-aligns and flushes, and returns the produced
// bytes plus the bit count encodeSectionData reports (bitenc.cpp:239,
// FDKaacEnc_encodeSectionData). Exported for the enc-bitstream parity oracle.
func EncodeSectionDataParity(blockType int, sections []SectionDesc, useVCB11 bool,
	bufBytes int) (out []byte, siBits int) {
	buf := make([]byte, bufBytes)
	bs := newWriteBitStream(buf)
	sd := buildSectionData(blockType, 0, sections)
	siBits = encodeSectionData(sd, bs, boolToUint(useVCB11))
	bs.byteAlignWrite(0)
	nBytes := (int(bs.getValidBitsWrite()) + 7) >> 3
	return buf[:nBytes], siBits
}

// EncodeScaleFactorDataParity serializes the DPCM-coded scalefactors, PNS
// energies and intensity scales into a fresh bufBytes-byte buffer, byte-aligns
// and flushes, and returns the produced bytes plus the bit count (or 1 on a
// coding-range error) encodeScaleFactorData reports (bitenc.cpp:292,
// FDKaacEnc_encodeScaleFactorData). maxValueInSfb / scalefac / noiseNrg /
// isScale are indexed by scalefactor band. Exported for the enc-bitstream
// parity oracle.
func EncodeScaleFactorDataParity(blockType, firstScf int, sections []SectionDesc,
	maxValueInSfb []uint, scalefac, noiseNrg, isScale []int, globalGain,
	bufBytes int) (out []byte, sfBits int) {
	buf := make([]byte, bufBytes)
	bs := newWriteBitStream(buf)
	sd := buildSectionData(blockType, firstScf, sections)
	sfBits = encodeScaleFactorData(maxValueInSfb, sd, scalefac, bs, noiseNrg,
		isScale, globalGain)
	bs.byteAlignWrite(0)
	nBytes := (int(bs.getValidBitsWrite()) + 7) >> 3
	return buf[:nBytes], sfBits
}

// EncodeMSInfoParity serializes the MS-stereo info into a fresh bufBytes-byte
// buffer, byte-aligns and flushes, and returns the produced bytes plus the
// static-bit count encodeMSInfo reports (bitenc.cpp:380,
// FDKaacEnc_encodeMSInfo). jsFlags is indexed by scalefactor band. Exported for
// the enc-bitstream parity oracle.
func EncodeMSInfoParity(sfbCnt, grpSfb, maxSfb, msDigest int, jsFlags []int,
	bufBytes int) (out []byte, msBits int) {
	buf := make([]byte, bufBytes)
	bs := newWriteBitStream(buf)
	msBits = encodeMSInfo(sfbCnt, grpSfb, maxSfb, msDigest, jsFlags, bs)
	bs.byteAlignWrite(0)
	nBytes := (int(bs.getValidBitsWrite()) + 7) >> 3
	return buf[:nBytes], msBits
}

// TnsFilterDesc describes one TNS filter for the TNS parity wrappers.
type TnsFilterDesc struct {
	Length    int
	Order     int
	Direction int
	Coef      []int16 // length Order
}

// TnsWindowDesc describes one TNS window (its filters + coef resolution).
type TnsWindowDesc struct {
	CoefRes int // 3 or 4
	Filters []TnsFilterDesc
}

// buildTnsInfo assembles a TnsInfo from per-window descriptors for the TNS
// parity wrappers. windows has one entry per window (1 for long, TRANS_FAC for
// short).
func buildTnsInfo(windows []TnsWindowDesc) *TnsInfo {
	ti := new(TnsInfo)
	for w, win := range windows {
		ti.NumOfFilters[w] = len(win.Filters)
		ti.CoefRes[w] = win.CoefRes
		for f, filt := range win.Filters {
			ti.Length[w][f] = filt.Length
			ti.Order[w][f] = filt.Order
			ti.Direction[w][f] = filt.Direction
			for k := 0; k < filt.Order && k < len(filt.Coef); k++ {
				ti.Coef[w][f][k] = filt.Coef[k]
			}
		}
	}
	return ti
}

// EncodeTnsDataPresentParity serializes the one-bit TNS-present flag into a
// fresh bufBytes-byte buffer, byte-aligns and flushes, and returns the produced
// bytes plus 1 (bitenc.cpp:434, FDKaacEnc_encodeTnsDataPresent). Exported for
// the enc-bitstream parity oracle.
func EncodeTnsDataPresentParity(blockType int, windows []TnsWindowDesc,
	bufBytes int) (out []byte, statBits int) {
	buf := make([]byte, bufBytes)
	bs := newWriteBitStream(buf)
	statBits = encodeTnsDataPresent(buildTnsInfo(windows), blockType, bs)
	bs.byteAlignWrite(0)
	nBytes := (int(bs.getValidBitsWrite()) + 7) >> 3
	return buf[:nBytes], statBits
}

// EncodeTnsDataParity serializes the TNS filter orders and coefficients into a
// fresh bufBytes-byte buffer, byte-aligns and flushes, and returns the produced
// bytes plus the static-bit count encodeTnsData reports (bitenc.cpp:465,
// FDKaacEnc_encodeTnsData). Exported for the enc-bitstream parity oracle.
func EncodeTnsDataParity(blockType int, windows []TnsWindowDesc,
	bufBytes int) (out []byte, tnsBits int) {
	buf := make([]byte, bufBytes)
	bs := newWriteBitStream(buf)
	tnsBits = encodeTnsData(buildTnsInfo(windows), blockType, bs)
	bs.byteAlignWrite(0)
	nBytes := (int(bs.getValidBitsWrite()) + 7) >> 3
	return buf[:nBytes], tnsBits
}

// EncodeSpectralDataParity Huffman-encodes the spectral data of every section
// into a fresh bufBytes-byte buffer, byte-aligns and flushes, and returns the
// produced bytes plus the bit count encodeSpectralData reports (bitenc.cpp:127,
// FDKaacEnc_encodeSpectralData). sfbOffset is indexed by scalefactor band;
// quantSpectrum holds the quantized coefficients. Exported for the enc-bitstream
// parity oracle.
func EncodeSpectralDataParity(blockType int, sections []SectionDesc,
	sfbOffset []int, quantSpectrum []int16, bufBytes int) (out []byte, specBits int) {
	buf := make([]byte, bufBytes)
	bs := newWriteBitStream(buf)
	sd := buildSectionData(blockType, 0, sections)
	specBits = encodeSpectralData(sfbOffset, sd, quantSpectrum, bs)
	bs.byteAlignWrite(0)
	nBytes := (int(bs.getValidBitsWrite()) + 7) >> 3
	return buf[:nBytes], specBits
}

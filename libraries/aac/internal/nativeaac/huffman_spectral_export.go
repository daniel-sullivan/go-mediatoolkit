// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// This file exposes thin exported wrappers around the unexported plain
// spectral Huffman decode kernels (bitstream.go, huffman_spectral.go) so the
// cgo parity oracle in internal/parity_tests/huffman-spectral-decode can drive
// them without being in-package. The wrappers add no logic: each forwards 1:1
// to the ported kernel under test. They exist solely for the parity harness —
// the production decode path uses the unexported forms directly.

// HuffBitReader is an exported handle over the unexported MSB-first cache bit
// reader (bitStream) the plain Huffman path observes.
type HuffBitReader struct {
	bs bitStream
}

// NewHuffBitReader initialises a reader over pBuffer with validBits valid
// bits. bufSize must be a power of two (the FDK FDK_BITBUF invariant).
//
// Wraps initBitStream (bitstream.go).
func NewHuffBitReader(pBuffer []byte, bufSize, validBits uint32) *HuffBitReader {
	r := new(HuffBitReader)
	initBitStream(&r.bs, pBuffer, bufSize, validBits)
	return r
}

// BitNdx returns the byte-buffer bit index, for cross-checking bit-consumption
// position against the C oracle.
func (r *HuffBitReader) BitNdx() uint32 {
	// The observable read position is the buffer index minus the bits still
	// held unconsumed in the 32-bit cache, mirroring how the C side computes
	// FDKgetBitCnt (bitNdx - BitsInCache).
	return r.bs.bitBuf.bitNdx - r.bs.bitsInCache
}

// SkipBits advances the reader by n bits by consuming single bits, mirroring
// FDKpushFor for the test harness (lands at the same bit position the C oracle
// seeks to). Wraps readBit.
func (r *HuffBitReader) SkipBits(n uint32) {
	for i := uint32(0); i < n; i++ {
		r.bs.readBit()
	}
}

// DecodeHuffmanWordCB walks one codeword for the spectral codebook cb (1..11)
// via the optimised read2Bits tree walker. Wraps decodeHuffmanWordCB.
func (r *HuffBitReader) DecodeHuffmanWordCB(cb int) int {
	return decodeHuffmanWordCB(&r.bs, aacCodeBookDescriptionTable[cb].codeBook)
}

// DecodeHuffmanWord walks one codeword for the spectral codebook cb (1..11)
// via the non-CB tree walker. Wraps decodeHuffmanWord.
func (r *HuffBitReader) DecodeHuffmanWord(cb int) int {
	return decodeHuffmanWord(&r.bs, aacCodeBookDescriptionTable[cb].codeBook)
}

// GetEscape resolves an escape sequence for quantized coefficient q. Wraps
// getEscape.
func (r *HuffBitReader) GetEscape(q int) int {
	return getEscape(&r.bs, q)
}

// ReadSpectralData runs the non-HCR plain-Huffman branch of
// readSpectralData over the reader, writing the unpacked int32 spectrum into
// spectrum. codeBook is the flat per-(group*16+band) codebook array,
// bandOffsets the scalefactor-band offset table, windowGroupLen the per-group
// window count, granuleLength the per-window stride, transmittedBands the
// transmitted scalefactor-band count.
func (r *HuffBitReader) ReadSpectralData(codeBook []byte, bandOffsets []int16,
	windowGroupLen []int, granuleLength, transmittedBands int, spectrum []int32) {
	in := &spectralInput{
		codeBook:         codeBook,
		bandOffsets:      bandOffsets,
		windowGroups:     len(windowGroupLen),
		windowGroupLen:   windowGroupLen,
		granuleLength:    granuleLength,
		transmittedBands: transmittedBands,
	}
	readSpectralData(&r.bs, in, spectrum)
}

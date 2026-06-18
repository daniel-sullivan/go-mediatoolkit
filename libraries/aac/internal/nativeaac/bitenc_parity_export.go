// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

// This file exposes the bitstream-encode port (the FDK bit WRITER plus the
// spectral / scalefactor Huffman emitters) to its parity oracle, which lives in
// a separate package (internal/parity_tests/bitstream-encode) and so cannot
// reach the unexported bitStream type, newWriteBitStream, CodeValues' bitStream
// receiver or CodeScalefactorDelta across the package boundary. Mirrors the
// FindSyncwordParity / DecodeHeaderParity bridge in adts_parity_export.go and
// the CalculateChaosMeasure bridge in psy_chaosmeasure.go. Not part of the
// shipping surface — purely a test seam.
//
// The seam drives the writer end to end (FDKwriteBits -> FDK_put ring store ->
// FDKsyncCache -> FDKbyteAlign) so the comparison covers the exact code path
// the raw-data-block assembler (bitenc.go) takes, and returns the produced
// buffer plus the bit count both sides report.

package nativeaac

// CodeValuesParity Huffman-encodes width spectral coefficients of one section
// with codeBook into a fresh power-of-two byte buffer of bufBytes bytes (which
// must hold the output), then byte-aligns and flushes. It returns the buffer
// truncated to the number of bytes the bits occupy and the FDKgetValidBits
// count. Exported for the bitstream-encode parity oracle; bufBytes mirrors the
// buffer size the C oracle hands FDKinitBitStream so both ring buffers wrap
// identically.
func CodeValuesParity(values []int16, width, codeBook, bufBytes int) (out []byte, validBits int) {
	buf := make([]byte, bufBytes)
	bs := newWriteBitStream(buf)
	CodeValues(values, width, codeBook, bs)
	vb := int(bs.getValidBitsWrite())
	bs.byteAlignWrite(0)
	nBytes := (int(bs.getValidBitsWrite()) + 7) >> 3
	return buf[:nBytes], vb
}

// CodeScalefactorDeltaParity DPCM-encodes a single scalefactor delta into a
// fresh bufBytes-byte buffer, byte-aligns and flushes, and returns the produced
// bytes, the pre-alignment valid-bit count and the C-style range-error return
// (1 when |delta| exceeds the scalefactor codebook's largest absolute value,
// else 0). Exported for the bitstream-encode parity oracle.
func CodeScalefactorDeltaParity(delta, bufBytes int) (out []byte, validBits, rangeErr int) {
	buf := make([]byte, bufBytes)
	bs := newWriteBitStream(buf)
	rangeErr = CodeScalefactorDelta(delta, bs)
	vb := int(bs.getValidBitsWrite())
	bs.byteAlignWrite(0)
	nBytes := (int(bs.getValidBitsWrite()) + 7) >> 3
	return buf[:nBytes], vb, rangeErr
}

// WriteBitsParity replays a sequence of (value, numberOfBits) writes through
// FDKwriteBits into a fresh bufBytes-byte buffer, byte-aligns and flushes, and
// returns the produced bytes plus the pre-alignment valid-bit count. It pins
// the raw bit WRITER + FDK_put ring store directly (independent of the Huffman
// tables), so the oracle can fabricate arbitrary write patterns — including
// cache-spanning and ring-wrapping ones — and compare the resulting bytes
// bit-for-bit. values and widths must have equal length. Exported for the
// bitstream-encode parity oracle.
func WriteBitsParity(values []uint32, widths []uint32, bufBytes int) (out []byte, validBits int) {
	buf := make([]byte, bufBytes)
	bs := newWriteBitStream(buf)
	for i := range values {
		bs.writeBits(values[i], widths[i])
	}
	vb := int(bs.getValidBitsWrite())
	bs.byteAlignWrite(0)
	nBytes := (int(bs.getValidBitsWrite()) + 7) >> 3
	return buf[:nBytes], vb
}

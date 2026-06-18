// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Byte-alignment helper used by the data_stream_element read in the AAC-LC
// raw_data_block loop. Mirrors FDKbyteAlign relative to the buffer start
// (FDK_bitstream.h): consume bits up to the next byte boundary.

// bitPosition returns the absolute number of bits consumed from the buffer
// (FDKgetBitCnt == bitNdx - BitsInCache).
func (bs *bitStream) bitPosition() uint32 {
	return bs.bitBuf.bitNdx - bs.bitsInCache
}

// byteAlign consumes bits up to the next byte boundary (relative to the buffer
// start). The DSE alignment anchor in a raw_data_block is the buffer origin, so
// aligning on the absolute bit count is correct here.
func (bs *bitStream) byteAlign() {
	rem := bs.bitPosition() & 7
	if rem != 0 {
		bs.readBits(8 - rem)
	}
}

// bufferBytes / bufferSize expose the underlying byte buffer and its (power-of-
// two) byte length so a sibling reader (the SBR parser) can be constructed over
// the same data at a matching bit position.
func (bs *bitStream) bufferBytes() []byte { return bs.bitBuf.buffer }
func (bs *bitStream) bufferSize() uint32  { return bs.bitBuf.bufSize }

// skipBits advances the reader by n bits (consuming and discarding them). Used to
// re-sync the core reader past the bits an out-of-band SBR parse consumed.
func (bs *bitStream) skipBits(n uint32) {
	for n >= 8 {
		bs.readBits(8)
		n -= 8
	}
	if n > 0 {
		bs.readBits(n)
	}
}

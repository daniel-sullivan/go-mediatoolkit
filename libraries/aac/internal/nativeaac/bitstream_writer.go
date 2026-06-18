// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// Bitstream-encode area: the FDK bit WRITER.
//
// 1:1 port of the writer subset of the vendored libfdk-aac FDK_bitstream /
// FDK_bitbuffer interface that the AAC bitstream-encode functions drive:
// FDKwriteBits + its 32-bit cache, FDKsyncCache, FDKgetValidBits, FDKbyteAlign
// and the underlying FDK_put ring-buffer store. The reader half (get32 /
// readBits / …) lives in bitstream.go and shares the same bitBuf / bitStream
// structs and the bitMask / cacheBits constants declared there; only the
// write-side methods are added here, so the two directions coexist on one
// type the way the C FDK_BITSTREAM does.
//
// Integer-only kernel — pure shifting and masking — bit-identical regardless
// of build tag. Every method names its C counterpart as file:line.

// newWriteBitStream constructs a bitStream in writer mode over buf, which must
// have a power-of-two byte length (FDK_bitstream.h:135 FDKinitBitStream +
// FDK_bitbuffer.cpp:126 FDK_InitBitBuffer, with validBits = 0).
func newWriteBitStream(buf []byte) *bitStream {
	bs := new(bitStream)
	bs.bitBuf.buffer = buf
	bs.bitBuf.bufSize = uint32(len(buf))
	bs.bitBuf.bufBits = uint32(len(buf)) << 3
	return bs
}

// fdkPut stores numberOfBits of value into the ring buffer, MSB-first, wrapping
// bitNdx modulo bufBits (FDK_bitbuffer.cpp:248, FDK_put).
func (b *bitBuf) fdkPut(value, numberOfBits uint32) {
	if numberOfBits != 0 {
		byteOffset0 := b.bitNdx >> 3
		bitOffset := b.bitNdx & 0x7

		b.bitNdx = (b.bitNdx + numberOfBits) & (b.bufBits - 1)
		b.validBits += numberOfBits

		byteMask := b.bufSize - 1

		byteOffset1 := (byteOffset0 + 1) & byteMask
		byteOffset2 := (byteOffset0 + 2) & byteMask
		byteOffset3 := (byteOffset0 + 3) & byteMask

		// Create tmp containing free bits at the left border followed by bits
		// to write, LSB's are cleared, if available. Create mask to apply upon
		// all buffer bytes.
		tmp := (value << (32 - numberOfBits)) >> bitOffset
		mask := ^((bitMask[numberOfBits] << (32 - numberOfBits)) >> bitOffset)

		// read all 4 bytes from buffer and create a 32-bit cache
		cache := (uint32(b.buffer[byteOffset0]) << 24) |
			(uint32(b.buffer[byteOffset1]) << 16) |
			(uint32(b.buffer[byteOffset2]) << 8) |
			(uint32(b.buffer[byteOffset3]) << 0)

		cache = (cache & mask) | tmp
		b.buffer[byteOffset0] = byte(cache >> 24)
		b.buffer[byteOffset1] = byte(cache >> 16)
		b.buffer[byteOffset2] = byte(cache >> 8)
		b.buffer[byteOffset3] = byte(cache >> 0)

		if (bitOffset + numberOfBits) > 32 {
			byteOffset4 := (byteOffset0 + 4) & byteMask
			// remaining bits: in range 1..7
			// replace MSBits of next byte in buffer by LSBits of "value"
			bits := (bitOffset + numberOfBits) & 7
			cache = uint32(b.buffer[byteOffset4]) & (^(bitMask[bits] << (8 - bits)))
			cache |= value << (8 - bits)
			b.buffer[byteOffset4] = byte(cache)
		}
	}
}

// writeBits writes the low numberOfBits of value into the bit stream,
// MSB-first, flushing 32-bit words through the cache into the ring buffer
// (FDK_bitstream.h:342, FDKwriteBits). A nil receiver counts bits only.
func (bs *bitStream) writeBits(value, numberOfBits uint32) uint32 {
	validMask := bitMask[numberOfBits]

	if bs == nil {
		return numberOfBits
	}

	if (bs.bitsInCache + numberOfBits) < cacheBits {
		bs.bitsInCache += numberOfBits
		bs.cacheWord = (bs.cacheWord << numberOfBits) | (value & validMask)
	} else {
		// Put always 32 bits into memory
		// - fill cache's LSBits with MSBits of value
		// - store 32 bits in memory using subroutine
		// - fill remaining bits into cache's LSBits
		// - upper bits in cache are don't care

		// Compute number of bits to be filled into cache
		missingBits := int(cacheBits) - int(bs.bitsInCache)
		remainingBits := int(numberOfBits) - missingBits
		value = value & validMask
		// Avoid shift left by 32 positions
		var cacheWord uint32
		if missingBits == 32 {
			cacheWord = 0
		} else {
			cacheWord = bs.cacheWord << uint(missingBits)
		}
		cacheWord |= value >> uint(remainingBits)
		bs.bitBuf.fdkPut(cacheWord, 32)

		bs.cacheWord = value
		bs.bitsInCache = uint32(remainingBits)
	}

	return numberOfBits
}

// syncCacheWrite flushes any bits still resident in the writer cache into the
// ring buffer (FDK_bitstream.h:452, FDKsyncCache — the BS_WRITER branch).
func (bs *bitStream) syncCacheWrite() {
	if bs.bitsInCache != 0 {
		bs.bitBuf.fdkPut(bs.cacheWord, bs.bitsInCache)
	}
	bs.bitsInCache = 0
	bs.cacheWord = 0
}

// getValidBitsWrite flushes the cache and returns the number of bits written
// so far (FDK_bitstream.h:577, FDKgetValidBits — writer side).
func (bs *bitStream) getValidBitsWrite() uint32 {
	bs.syncCacheWrite()
	return bs.bitBuf.validBits
}

// byteAlignWrite pads the stream with zero bits up to a byte boundary, measured
// relative to alignmentAnchor (FDK_bitstream.h:495, FDKbyteAlign — writer side).
func (bs *bitStream) byteAlignWrite(alignmentAnchor uint32) {
	bs.syncCacheWrite()
	bs.bitBuf.fdkPut(0, (8-((bs.bitBuf.validBits-alignmentAnchor)&0x07))&0x07)
}

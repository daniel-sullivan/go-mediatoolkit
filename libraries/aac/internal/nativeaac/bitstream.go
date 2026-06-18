// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// This file ports the cache-level MSB-first bit reader that the plain
// spectral Huffman decoder observes, 1:1 from the vendored Fraunhofer
// FDK-AAC reference (libFDK/include/FDK_bitstream.h and the FDK_get32 word
// fetch in libFDK/src/FDK_bitbuffer.cpp). Only the read primitives consulted
// by the Huffman path are ported: get32 / readBits / read2Bits / readBit /
// pushBackCache. The full FDK bit buffer (write side, byte-aligned helpers,
// pushForward/pushBack across the buffer) is a separate area and is not
// ported here.
//
// Bit-consumption order is the observable contract for parity: this is an
// integer kernel and is bit-identical regardless of build tag. The backing
// Buffer must be a power-of-two byte length, matching the C FDK_BITBUF
// invariant (bufBits = bufSize<<3, BitNdx masked by bufBits-1).

// cacheBits is the FDK CACHE_BITS macro: the cache word is 32 bits wide.
//
// C counterpart: libFDK/include/FDK_bitstream.h:111 (#define CACHE_BITS 32).
const cacheBits = 32

// bitMask ports the C const UINT BitMask[33]
// (libFDK/src/FDK_bitbuffer.cpp:109): bitMask[n] has the low n bits set.
var bitMask = [33]uint32{
	0x0, 0x1, 0x3, 0x7, 0xf, 0x1f,
	0x3f, 0x7f, 0xff, 0x1ff, 0x3ff, 0x7ff,
	0xfff, 0x1fff, 0x3fff, 0x7fff, 0xffff, 0x1ffff,
	0x3ffff, 0x7ffff, 0xfffff, 0x1fffff, 0x3fffff, 0x7fffff,
	0xffffff, 0x1ffffff, 0x3ffffff, 0x7ffffff, 0xfffffff, 0x1fffffff,
	0x3fffffff, 0x7fffffff, 0xffffffff,
}

// bitBuf ports the FDK_BITBUF struct (libFDK/include/FDK_bitbuffer.h): the
// byte-buffer read state underneath the 32-bit cache.
type bitBuf struct {
	buffer    []byte // Buffer
	validBits uint32 // ValidBits
	readOffs  uint32 // ReadOffset
	writeOffs uint32 // WriteOffset
	bitNdx    uint32 // BitNdx
	bufSize   uint32 // bufSize (bytes)
	bufBits   uint32 // bufBits (bufSize << 3)
}

// bitStream ports the FDK_BITSTREAM struct
// (libFDK/include/FDK_bitstream.h:117): the 32-bit MSB-first read cache on
// top of a bitBuf.
type bitStream struct {
	cacheWord   uint32 // CacheWord
	bitsInCache uint32 // BitsInCache
	bitBuf      bitBuf // hBitBuf
}

// initBitStream initialises a reader over pBuffer with validBits valid bits.
//
// C counterparts: FDKinitBitStream (libFDK/include/FDK_bitstream.h:163) and
// FDK_InitBitBuffer (libFDK/src/FDK_bitbuffer.cpp). bufSize is the byte
// length of pBuffer and must be a power of two (the C asserts this).
func initBitStream(bs *bitStream, pBuffer []byte, bufSize, validBits uint32) {
	bs.bitBuf.validBits = validBits
	bs.bitBuf.readOffs = 0
	bs.bitBuf.writeOffs = 0
	bs.bitBuf.bitNdx = 0
	bs.bitBuf.buffer = pBuffer
	bs.bitBuf.bufSize = bufSize
	bs.bitBuf.bufBits = bufSize << 3

	// init cache
	bs.cacheWord = 0
	bs.bitsInCache = 0
}

// get32 fetches the next 32 bits (MSB-first) from the byte buffer, advancing
// the read index, exactly as the C scalar FDK_get32.
//
// C counterpart: libFDK/src/FDK_bitbuffer.cpp:181 (FDK_get32).
func (b *bitBuf) get32() uint32 {
	bitNdx := b.bitNdx + 32
	b.bitNdx = bitNdx & (b.bufBits - 1)
	b.validBits = uint32(int32(b.validBits) - 32)

	byteOffset := (bitNdx - 1) >> 3
	if bitNdx <= b.bufBits {
		cache := uint32(b.buffer[byteOffset-3])<<24 |
			uint32(b.buffer[byteOffset-2])<<16 |
			uint32(b.buffer[byteOffset-1])<<8 |
			uint32(b.buffer[byteOffset-0])

		if bitNdx = bitNdx & 7; bitNdx != 0 {
			cache = (cache >> (8 - bitNdx)) |
				(uint32(b.buffer[byteOffset-4]) << (24 + bitNdx))
		}
		return cache
	}

	byteMask := b.bufSize - 1
	cache := uint32(b.buffer[(byteOffset-3)&byteMask])<<24 |
		uint32(b.buffer[(byteOffset-2)&byteMask])<<16 |
		uint32(b.buffer[(byteOffset-1)&byteMask])<<8 |
		uint32(b.buffer[(byteOffset-0)&byteMask])

	if bitNdx = bitNdx & 7; bitNdx != 0 {
		cache = (cache >> (8 - bitNdx)) |
			(uint32(b.buffer[(byteOffset-4)&byteMask]) << (24 + bitNdx))
	}
	return cache
}

// readBits returns numberOfBits sequential bits, right-aligned.
//
// C counterpart: FDKreadBits (libFDK/include/FDK_bitstream.h:210).
func (bs *bitStream) readBits(numberOfBits uint32) uint32 {
	var bits uint32
	missingBits := int32(numberOfBits) - int32(bs.bitsInCache)

	if missingBits > 0 {
		if missingBits != 32 {
			bits = bs.cacheWord << uint32(missingBits)
		}
		bs.cacheWord = bs.bitBuf.get32()
		bs.bitsInCache += cacheBits
	}

	bs.bitsInCache -= numberOfBits

	return (bits | (bs.cacheWord >> bs.bitsInCache)) & bitMask[numberOfBits]
}

// readBit returns the next single bit, right-aligned.
//
// C counterpart: FDKreadBit (libFDK/include/FDK_bitstream.h:228).
func (bs *bitStream) readBit() uint32 {
	if bs.bitsInCache == 0 {
		bs.cacheWord = bs.bitBuf.get32()
		bs.bitsInCache = cacheBits - 1
		return bs.cacheWord >> 31
	}
	bs.bitsInCache--

	return (bs.cacheWord >> bs.bitsInCache) & 1
}

// read2Bits returns the next 2 bits, right-aligned — the optimised 2-bit
// form of FDKreadBits used by the Huffman word decoder.
//
// C counterpart: FDKread2Bits (libFDK/include/FDK_bitstream.h:248).
func (bs *bitStream) read2Bits() uint32 {
	var bits uint32
	missingBits := 2 - int32(bs.bitsInCache)
	if missingBits > 0 {
		bits = bs.cacheWord << uint32(missingBits)
		bs.cacheWord = bs.bitBuf.get32()
		bs.bitsInCache += cacheBits
	}

	bs.bitsInCache -= 2

	return (bits | (bs.cacheWord >> bs.bitsInCache)) & 0x3
}

// pushBackCache returns numberOfBits already-read bits to the cache.
//
// C counterpart: FDKpushBackCache (libFDK/include/FDK_bitstream.h:532).
func (bs *bitStream) pushBackCache(numberOfBits uint32) {
	bs.bitsInCache += numberOfBits
}

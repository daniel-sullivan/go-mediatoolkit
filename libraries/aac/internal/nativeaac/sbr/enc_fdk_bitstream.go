// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// A self-contained 1:1 port of the WRITER half of the vendored libfdk-aac
// FDK_BITSTREAM / FDK_BITBUF (libFDK/include/FDK_bitstream.h +
// libFDK/src/FDK_bitbuffer.cpp) that the SBR bitstream writer (bit_sbr.cpp)
// drives via cmonData->sbrBitbuf. It mirrors the AAC-core writer in
// internal/nativeaac (FDKwriteBits + the 32-bit cache + FDK_put ring buffer +
// FDKgetValidBits / FDKpushBack), but lives in this package so the SBR encode
// chain is self-contained and merge-clean. Integer-only: pure shift/mask,
// bit-identical regardless of build flags.
package sbr

const fdkCacheBits = 32 // CACHE_BITS

// fdkBitMask[n] has the low n bits set (FDK_bitbuffer.cpp:109, BitMask[33]).
var fdkBitMask = [33]uint32{
	0x00000000, 0x00000001, 0x00000003, 0x00000007, 0x0000000f, 0x0000001f,
	0x0000003f, 0x0000007f, 0x000000ff, 0x000001ff, 0x000003ff, 0x000007ff,
	0x00000fff, 0x00001fff, 0x00003fff, 0x00007fff, 0x0000ffff, 0x0001ffff,
	0x0003ffff, 0x0007ffff, 0x000fffff, 0x001fffff, 0x003fffff, 0x007fffff,
	0x00ffffff, 0x01ffffff, 0x03ffffff, 0x07ffffff, 0x0fffffff, 0x1fffffff,
	0x3fffffff, 0x7fffffff, 0xffffffff,
}

// fdkBitBuf is the 1:1 port of FDK_BITBUF (FDK_bitbuffer.h).
type fdkBitBuf struct {
	buffer    []byte
	validBits uint32
	bitNdx    uint32
	bufSize   uint32 // in bytes
	bufBits   uint32 // in bits
}

// FdkBitStream is the 1:1 port of FDK_BITSTREAM in writer mode (the SBR
// cmonData->sbrBitbuf). Exported so the encode driver + e2e can drive it.
type FdkBitStream struct {
	bitBuf      fdkBitBuf
	cacheWord   uint32
	bitsInCache uint32
}

// NewFdkWriteBitStream constructs a writer-mode FdkBitStream over buf (whose
// byte length must be a power of two), mirroring FDKinitBitStream +
// FDK_InitBitBuffer with validBits == 0.
func NewFdkWriteBitStream(buf []byte) *FdkBitStream {
	bs := new(FdkBitStream)
	bs.bitBuf.buffer = buf
	bs.bitBuf.bufSize = uint32(len(buf))
	bs.bitBuf.bufBits = uint32(len(buf)) << 3
	return bs
}

// fdkPut stores numberOfBits of value MSB-first into the ring buffer, wrapping
// bitNdx modulo bufBits (FDK_bitbuffer.cpp:248, FDK_put).
func (b *fdkBitBuf) fdkPut(value, numberOfBits uint32) {
	if numberOfBits != 0 {
		byteOffset0 := b.bitNdx >> 3
		bitOffset := b.bitNdx & 0x7

		b.bitNdx = (b.bitNdx + numberOfBits) & (b.bufBits - 1)
		b.validBits += numberOfBits

		byteMask := b.bufSize - 1

		byteOffset1 := (byteOffset0 + 1) & byteMask
		byteOffset2 := (byteOffset0 + 2) & byteMask
		byteOffset3 := (byteOffset0 + 3) & byteMask

		tmp := (value << (32 - numberOfBits)) >> bitOffset
		mask := ^((fdkBitMask[numberOfBits] << (32 - numberOfBits)) >> bitOffset)

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
			bits := (bitOffset + numberOfBits) & 7
			cache = uint32(b.buffer[byteOffset4]) & (^(fdkBitMask[bits] << (8 - bits)))
			cache |= value << (8 - bits)
			b.buffer[byteOffset4] = byte(cache)
		}
	}
}

// Reset clears the writer state (FDKresetBitbuffer, BS_WRITER): validBits and
// bitNdx to 0, cache empty. The backing buffer is retained.
func (bs *FdkBitStream) Reset() {
	bs.bitBuf.validBits = 0
	bs.bitBuf.bitNdx = 0
	bs.cacheWord = 0
	bs.bitsInCache = 0
}

// WriteBits writes the low numberOfBits of value MSB-first, flushing 32-bit
// words through the cache (FDK_bitstream.h:342, FDKwriteBits). A nil receiver
// counts only. Returns numberOfBits.
func (bs *FdkBitStream) WriteBits(value, numberOfBits uint32) uint32 {
	validMask := fdkBitMask[numberOfBits]

	if bs == nil {
		return numberOfBits
	}

	if (bs.bitsInCache + numberOfBits) < fdkCacheBits {
		bs.bitsInCache += numberOfBits
		bs.cacheWord = (bs.cacheWord << numberOfBits) | (value & validMask)
	} else {
		missingBits := int(fdkCacheBits) - int(bs.bitsInCache)
		remainingBits := int(numberOfBits) - missingBits
		value = value & validMask
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

// syncCache flushes the writer cache into the ring buffer (FDK_bitstream.h:452,
// FDKsyncCache — the BS_WRITER branch).
func (bs *FdkBitStream) syncCache() {
	if bs.bitsInCache != 0 {
		bs.bitBuf.fdkPut(bs.cacheWord, bs.bitsInCache)
		bs.bitsInCache = 0
		bs.cacheWord = 0
	}
}

// GetValidBits flushes the cache and returns the total bits written so far
// (FDK_bitstream.h:577, FDKgetValidBits — writer side).
func (bs *FdkBitStream) GetValidBits() uint32 {
	bs.syncCache()
	return bs.bitBuf.validBits
}

// PushBack returns numberOfBits already-written bits (the writer/BS_WRITER
// branch of FDKpushBack -> FDK_pushBack with config==1) so a Count helper can
// rewind after a trial write (FDK_bitstream.h:538 / FDK_bitbuffer.cpp:338).
func (bs *FdkBitStream) PushBack(numberOfBits uint32) {
	bs.syncCache()
	bs.bitBuf.validBits = uint32(int(bs.bitBuf.validBits) - int(numberOfBits))
	bs.bitBuf.bitNdx = uint32(int(bs.bitBuf.bitNdx)-int(numberOfBits)) & (bs.bitBuf.bufBits - 1)
}

// ByteAlignWrite pads with zero bits to the next byte boundary relative to
// alignmentAnchor (FDK_bitstream.h:495, FDKbyteAlign — writer side).
func (bs *FdkBitStream) ByteAlignWrite(alignmentAnchor uint32) {
	bs.syncCache()
	valid := bs.bitBuf.validBits
	align := (alignmentAnchor - valid) & 7
	if align != 0 {
		bs.WriteBits(0, align)
		bs.syncCache()
	}
}

// Bytes returns the written payload as a byte slice (flushing the cache first).
// It returns ceil(validBits/8) bytes from the start of the buffer.
func (bs *FdkBitStream) Bytes() []byte {
	bs.syncCache()
	n := (bs.bitBuf.validBits + 7) >> 3
	return bs.bitBuf.buffer[:n]
}

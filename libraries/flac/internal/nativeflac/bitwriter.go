package nativeflac

import (
	"encoding/binary"
)

// 1:1 port of libflac/src/libFLAC/bitwriter.c.
//
// Bit-level writer that accumulates an MSB-first bit stream into a
// growable word buffer. This is the write-side mirror of bitreader.c.
//
// The vendored libFLAC builds with ENABLE_64_BIT_WORDS == 1 (see
// libflac/config.h), so bwword is FLAC__uint64 and FLAC__BITS_PER_WORD
// is 64. This port follows that configuration exactly: the buffer is a
// []uint64 of big-endian words, the accumulator is a uint64 holding
// right-justified bits, and words are byte-swapped to host order on
// little-endian hosts when stored. Because Go's encoding/binary makes
// host endianness explicit, the buffer always stores words in native
// order and getBuffer reproduces libFLAC's exact byte layout by
// emitting each word big-endian. See bitwriter.c:53-83 for the
// word-size selection and SWAP_BE_WORD_TO_HOST.
//
// FLAC__bwtemp / FLAC__TEMP_BITS (64) and FLAC__HALF_TEMP_BITS (32) are
// used by the wide-accumulator rice-block path; the 64-bit-word branch
// is ported below (bitwriter.c:551-570, 593-605, 684-699).

const (
	// bitwriterDefaultCapacity is the initial buffer size in words.
	// libFLAC: 32768 / sizeof(bwword) = 32768 / 8 = 4096 words
	// (bitwriter.c:91).
	bitwriterDefaultCapacity = 32768 / bwBytesPerWord

	// bitwriterDefaultGrowFraction: grow by >> 2 (1/4th) of current
	// size (bitwriter.c:93).
	bitwriterDefaultGrowFraction = 2

	// bwBytesPerWord is sizeof(bwword) for the 64-bit-word build
	// (bitwriter.c:72).
	bwBytesPerWord = 8
	// bwBitsPerWord is FLAC__BITS_PER_WORD (bitwriter.c:73).
	bwBitsPerWord = 64
	// bwTempBits is FLAC__TEMP_BITS (bitwriter.c:74).
	bwTempBits = 64
	// bwHalfTempBits is FLAC__HALF_TEMP_BITS (bitwriter.c:75).
	bwHalfTempBits = 32

	// streamMetadataLengthLen mirrors FLAC__STREAM_METADATA_LENGTH_LEN
	// (== 24). bitwriter_grow_ refuses to grow past 1<<24 bytes.
	streamMetadataLengthLen = 24
)

// BitWriter is the Go counterpart of FLAC__BitWriter (bitwriter.c:98).
type BitWriter struct {
	buffer   []uint64 // word buffer; words are stored host-order
	accum    uint64   // accumulator; bits are right-justified
	capacity uint32   // capacity of buffer in words
	words    uint32   // # of complete words in buffer
	bits     uint32   // # of used bits in accum
}

// NewBitWriter — port of FLAC__bitwriter_new (bitwriter.c:157). calloc
// zeroes all members; Go's zero value matches.
func NewBitWriter() *BitWriter { return new(BitWriter) }

// Delete — port of FLAC__bitwriter_delete (bitwriter.c:164).
func (bw *BitWriter) Delete() {
	bw.Free()
}

// Init — port of FLAC__bitwriter_init (bitwriter.c:178). Allocates the
// default-capacity word buffer.
func (bw *BitWriter) Init() bool {
	bw.words = 0
	bw.bits = 0
	bw.capacity = bitwriterDefaultCapacity
	bw.buffer = make([]uint64, bw.capacity)
	return true
}

// Free — port of FLAC__bitwriter_free (bitwriter.c:191).
func (bw *BitWriter) Free() {
	bw.buffer = nil
	bw.capacity = 0
	bw.words = 0
	bw.bits = 0
}

// Clear — port of FLAC__bitwriter_clear (bitwriter.c:202).
func (bw *BitWriter) Clear() {
	bw.words = 0
	bw.bits = 0
}

// grow — port of bitwriter_grow_ (bitwriter.c:110). Grows the buffer to
// hold bitsToAdd additional bits. WATCHOUT: only ever grows.
func (bw *BitWriter) grow(bitsToAdd uint32) bool {
	// calculate total words needed to store 'bits_to_add' additional bits
	newCapacity := bw.words + ((bw.bits + bitsToAdd + bwBitsPerWord - 1) / bwBitsPerWord)

	// it's possible (due to pessimism in the growth estimation that
	// leads to this call) that we don't actually need to grow
	if bw.capacity >= newCapacity {
		return true
	}

	if uint64(newCapacity)*bwBytesPerWord > (1 << streamMetadataLengthLen) {
		// Requested new capacity is larger than the largest possible
		// metadata block; give up to prevent crashing.
		return false
	}

	// As reallocation can be quite expensive, grow exponentially
	if (newCapacity - bw.capacity) < (bw.capacity >> bitwriterDefaultGrowFraction) {
		newCapacity = bw.capacity + (bw.capacity >> bitwriterDefaultGrowFraction)
	}

	newBuffer := make([]uint64, newCapacity)
	copy(newBuffer, bw.buffer[:bw.words])
	bw.buffer = newBuffer
	bw.capacity = newCapacity
	return true
}

// GetWriteCRC16 — port of FLAC__bitwriter_get_write_crc16
// (bitwriter.c:207). Caller must be byte-aligned.
func (bw *BitWriter) GetWriteCRC16() (crc uint16, ok bool) {
	buffer, ok := bw.GetBuffer()
	if !ok {
		return 0, false
	}
	crc = CRC16(buffer)
	bw.ReleaseBuffer()
	return crc, true
}

// GetWriteCRC8 — port of FLAC__bitwriter_get_write_crc8
// (bitwriter.c:222). Caller must be byte-aligned.
func (bw *BitWriter) GetWriteCRC8() (crc byte, ok bool) {
	buffer, ok := bw.GetBuffer()
	if !ok {
		return 0, false
	}
	crc = CRC8(buffer)
	bw.ReleaseBuffer()
	return crc, true
}

// IsByteAligned — port of FLAC__bitwriter_is_byte_aligned
// (bitwriter.c:237).
func (bw *BitWriter) IsByteAligned() bool { return (bw.bits & 7) == 0 }

// GetInputBitsUnconsumed — port of
// FLAC__bitwriter_get_input_bits_unconsumed (bitwriter.c:242).
func (bw *BitWriter) GetInputBitsUnconsumed() uint32 {
	return bw.words*bwBitsPerWord + bw.bits
}

// GetBuffer — port of FLAC__bitwriter_get_buffer (bitwriter.c:247).
// Returns the byte view of the written stream. Caller must be
// byte-aligned. If there are leftover bits in the accumulator, they are
// flushed into one extra word (without disturbing accum/bits) so the
// returned byte slice includes the trailing partial-but-byte-aligned
// word's bytes.
func (bw *BitWriter) GetBuffer() (buffer []byte, ok bool) {
	// double protection
	if bw.bits&7 != 0 {
		return nil, false
	}
	// if we have bits in the accumulator we have to flush those to the
	// buffer first
	if bw.bits != 0 {
		if bw.words == bw.capacity && !bw.grow(bwBitsPerWord) {
			return nil, false
		}
		// append bits as complete word to buffer, but don't change
		// bw->accum or bw->bits
		bw.buffer[bw.words] = bw.accum << (bwBitsPerWord - bw.bits)
	}
	// now we can just return what we have. libFLAC returns a pointer
	// into the word buffer (already host-order, the SWAP_BE_WORD_TO_HOST
	// on store made each stored word big-endian-in-memory). Reproduce
	// that exact byte layout by serialising each word big-endian.
	nbytes := bwBytesPerWord*int(bw.words) + int(bw.bits>>3)
	out := make([]byte, bwBytesPerWord*int(bw.words)+bwBytesPerWord)
	for i := uint32(0); i < bw.words; i++ {
		binary.BigEndian.PutUint64(out[i*bwBytesPerWord:], bw.buffer[i])
	}
	if bw.bits != 0 {
		binary.BigEndian.PutUint64(out[bw.words*bwBytesPerWord:], bw.buffer[bw.words])
	}
	return out[:nbytes], true
}

// ReleaseBuffer — port of FLAC__bitwriter_release_buffer
// (bitwriter.c:267). No-op.
func (bw *BitWriter) ReleaseBuffer() {}

// WriteZeroes — port of FLAC__bitwriter_write_zeroes (bitwriter.c:275).
func (bw *BitWriter) WriteZeroes(nbits uint32) bool {
	if nbits == 0 {
		return true
	}
	// slightly pessimistic size check
	if bw.capacity <= bw.words+nbits && !bw.grow(nbits) {
		return false
	}
	// first part gets to word alignment
	if bw.bits != 0 {
		n := bwBitsPerWord - bw.bits
		if n > nbits {
			n = nbits
		}
		bw.accum <<= n
		nbits -= n
		bw.bits += n
		if bw.bits == bwBitsPerWord {
			bw.buffer[bw.words] = bw.accum
			bw.words++
			bw.bits = 0
		} else {
			return true
		}
	}
	// do whole words
	for nbits >= bwBitsPerWord {
		bw.buffer[bw.words] = 0
		bw.words++
		nbits -= bwBitsPerWord
	}
	// do any leftovers
	if nbits > 0 {
		bw.accum = 0
		bw.bits = nbits
	}
	return true
}

// writeRawUint32NoCheck — port of
// FLAC__bitwriter_write_raw_uint32_nocheck (bitwriter.c:313).
func (bw *BitWriter) writeRawUint32NoCheck(val uint32, nbits uint32) bool {
	if bw.buffer == nil {
		return false
	}
	if nbits > 32 {
		return false
	}
	if nbits == 0 {
		return true
	}

	// slightly pessimistic size check
	if bw.capacity <= bw.words+nbits && !bw.grow(nbits) {
		return false
	}

	left := uint32(bwBitsPerWord) - bw.bits
	if nbits < left {
		bw.accum <<= nbits
		bw.accum |= uint64(val)
		bw.bits += nbits
	} else if bw.bits != 0 {
		// WATCHOUT: if bw.bits == 0, left==FLAC__BITS_PER_WORD and
		// bw.accum<<=left is a NOP instead of setting to 0
		bw.accum <<= left
		bw.bits = nbits - left
		bw.accum |= uint64(val >> bw.bits)
		bw.buffer[bw.words] = bw.accum
		bw.words++
		bw.accum = uint64(val) // unused top bits can contain garbage
	} else {
		// at this point bits == FLAC__BITS_PER_WORD == 64 ... but for the
		// 64-bit-word build nbits is at most 32 and left==64, so nbits <
		// left is always true above; this branch is only reachable when
		// FLAC__BITS_PER_WORD == 32. Kept for structural fidelity.
		bw.buffer[bw.words] = uint64(val)
		bw.words++
	}

	return true
}

// WriteRawUint32 — port of FLAC__bitwriter_write_raw_uint32
// (bitwriter.c:354).
func (bw *BitWriter) WriteRawUint32(val uint32, nbits uint32) bool {
	// check that unused bits are unset
	if nbits < 32 && (val>>nbits) != 0 {
		return false
	}
	return bw.writeRawUint32NoCheck(val, nbits)
}

// WriteRawInt32 — port of FLAC__bitwriter_write_raw_int32
// (bitwriter.c:363).
func (bw *BitWriter) WriteRawInt32(val int32, nbits uint32) bool {
	uval := uint32(val)
	// zero-out unused bits
	if nbits < 32 {
		uval &= ^(uint32(0xffffffff) << nbits)
	}
	return bw.writeRawUint32NoCheck(uval, nbits)
}

// WriteRawUint64 — port of FLAC__bitwriter_write_raw_uint64
// (bitwriter.c:372).
func (bw *BitWriter) WriteRawUint64(val uint64, nbits uint32) bool {
	if nbits > 32 {
		return bw.WriteRawUint32(uint32(val>>32), nbits-32) &&
			bw.writeRawUint32NoCheck(uint32(val), 32)
	}
	return bw.WriteRawUint32(uint32(val), nbits)
}

// WriteRawInt64 — port of FLAC__bitwriter_write_raw_int64
// (bitwriter.c:384).
func (bw *BitWriter) WriteRawInt64(val int64, nbits uint32) bool {
	uval := uint64(val)
	// zero-out unused bits
	if nbits < 64 {
		uval &= ^(^uint64(0) << nbits)
	}
	return bw.WriteRawUint64(uval, nbits)
}

// WriteRawUint32LittleEndian — port of
// FLAC__bitwriter_write_raw_uint32_little_endian (bitwriter.c:393).
func (bw *BitWriter) WriteRawUint32LittleEndian(val uint32) bool {
	if !bw.writeRawUint32NoCheck(val&0xff, 8) {
		return false
	}
	if !bw.writeRawUint32NoCheck((val>>8)&0xff, 8) {
		return false
	}
	if !bw.writeRawUint32NoCheck((val>>16)&0xff, 8) {
		return false
	}
	if !bw.writeRawUint32NoCheck(val>>24, 8) {
		return false
	}
	return true
}

// WriteByteBlock — port of FLAC__bitwriter_write_byte_block
// (bitwriter.c:409).
func (bw *BitWriter) WriteByteBlock(vals []byte) bool {
	nvals := uint32(len(vals))
	// grow capacity upfront to prevent constant reallocation during writes
	if bw.capacity <= bw.words+nvals/(bwBitsPerWord/8)+1 && !bw.grow(nvals*8) {
		return false
	}
	for i := uint32(0); i < nvals; i++ {
		if !bw.writeRawUint32NoCheck(uint32(vals[i]), 8) {
			return false
		}
	}
	return true
}

// WriteUnaryUnsigned — port of FLAC__bitwriter_write_unary_unsigned
// (bitwriter.c:426).
func (bw *BitWriter) WriteUnaryUnsigned(val uint32) bool {
	if val < 32 {
		val++
		return bw.writeRawUint32NoCheck(1, val)
	}
	return bw.WriteZeroes(val) && bw.writeRawUint32NoCheck(1, 1)
}

// wideAccumToBW — port of the WIDE_ACCUM_TO_BW macro for the
// 64-bit-word build (bitwriter.c:553-568). Mutates wideAccum,
// bitpointer, and the bitwriter accumulator/buffer in place.
func (bw *BitWriter) wideAccumToBW(wideAccum *uint64, bitpointer *uint32) {
	if bw.bits == 0 {
		bw.accum = *wideAccum >> bwHalfTempBits
		*wideAccum <<= bwHalfTempBits
		bw.bits = bwHalfTempBits
	} else {
		bw.accum <<= bwHalfTempBits
		bw.accum += *wideAccum >> bwHalfTempBits
		bw.buffer[bw.words] = bw.accum
		bw.words++
		*wideAccum <<= bwHalfTempBits
		bw.bits = 0
	}
	*bitpointer += bwHalfTempBits
}

// WriteRiceSignedBlock — port of
// FLAC__bitwriter_write_rice_signed_block (bitwriter.c:572), 64-bit-word
// build.
func (bw *BitWriter) WriteRiceSignedBlock(vals []int32, parameter uint32) bool {
	nvals := uint32(len(vals))
	mask1 := uint32(0xffffffff) << parameter // val|=mask1 sets the stop bit above it...
	mask2 := uint32(0xffffffff) >> (31 - parameter)
	lsbits := 1 + parameter
	var wideAccum uint64
	bitpointer := uint32(bwTempBits)

	// 64-bit-word prologue (bitwriter.c:593-605).
	if bw.bits > 0 && bw.bits < bwHalfTempBits {
		bitpointer -= bw.bits
		wideAccum = bw.accum << bitpointer
		bw.bits = 0
	} else if bw.bits > bwHalfTempBits {
		bitpointer -= bw.bits - bwHalfTempBits
		wideAccum = bw.accum << bitpointer
		bw.accum >>= bw.bits - bwHalfTempBits
		bw.bits = bwHalfTempBits
	}

	// Reserve one FLAC__TEMP_BITS per symbol.
	if bw.capacity*bwBitsPerWord <= bw.words*bwBitsPerWord+nvals*bwTempBits+bw.bits &&
		!bw.grow(nvals*bwTempBits) {
		return false
	}

	// The up-front reservation above guarantees FLAC__TEMP_BITS of slack per
	// symbol, so the whole-symbol fast path never re-checks capacity; only the
	// rare oversize-symbol split path below re-checks (kept for safety). idx
	// walks vals directly; nvals is the remaining-symbol count used by the
	// split-path reservation, so it still decrements once per symbol.
	for idx := uint32(0); idx < uint32(len(vals)); idx++ {
		// fold signed to uint32_t; actual formula: negative(v)? -2v-1 : 2v
		v := vals[idx]
		uval := uint32(v) << 1
		uval ^= uint32(v >> 31)

		msbits := uval >> parameter
		totalBits := lsbits + msbits

		uval |= mask1 // set stop bit
		uval &= mask2 // mask off unused top bits

		if totalBits <= bitpointer {
			// There is room enough to store the symbol whole at once
			wideAccum |= uint64(uval) << (bitpointer - totalBits)
			bitpointer -= totalBits
			if bitpointer <= bwHalfTempBits {
				bw.wideAccumToBW(&wideAccum, &bitpointer)
			}
			nvals--
			continue
		}
		// The symbol needs to be split. First check for space.
		{
			if totalBits > bwTempBits {
				oversizeInBits := totalBits - bwTempBits
				capacityNeeded := bw.words*bwBitsPerWord + bw.bits + nvals*bwTempBits + oversizeInBits
				if bw.capacity*bwBitsPerWord <= capacityNeeded &&
					!bw.grow(nvals*bwTempBits+oversizeInBits) {
					return false
				}
			}
			if msbits > bitpointer {
				// A lot of 0 bits to write; first align with bitwriter word
				msbits -= bitpointer - bwHalfTempBits
				bitpointer = bwHalfTempBits
				bw.wideAccumToBW(&wideAccum, &bitpointer)
				for msbits > bitpointer {
					// accumulator is already zero
					bw.wideAccumToBW(&wideAccum, &bitpointer)
					bitpointer -= bwHalfTempBits
					msbits -= bwHalfTempBits
				}
				bitpointer -= msbits
				if bitpointer <= bwHalfTempBits {
					bw.wideAccumToBW(&wideAccum, &bitpointer)
				}
			} else {
				bitpointer -= msbits
				if bitpointer <= bwHalfTempBits {
					bw.wideAccumToBW(&wideAccum, &bitpointer)
				}
			}
			// The lsbs + stop bit always fit 32 bit
			wideAccum |= uint64(uval) << (bitpointer - lsbits)
			bitpointer -= lsbits
			if bitpointer <= bwHalfTempBits {
				bw.wideAccumToBW(&wideAccum, &bitpointer)
			}
		}
		nvals--
	}

	// Now fixup remainder of wide_accum (bitwriter.c:684-699).
	if bitpointer < bwTempBits {
		if bw.bits == 0 {
			bw.accum = wideAccum >> bitpointer
			bw.bits = bwTempBits - bitpointer
		} else if bw.bits == bwHalfTempBits {
			bw.accum <<= bwTempBits - bitpointer
			bw.accum |= wideAccum >> bitpointer
			bw.bits = bwHalfTempBits + bwTempBits - bitpointer
		}
		// else: FLAC__ASSERT(0) — unreachable
	}

	return true
}

// WriteUTF8Uint32 — port of FLAC__bitwriter_write_utf8_uint32
// (bitwriter.c:829). Handles 31-bit values.
func (bw *BitWriter) WriteUTF8Uint32(val uint32) bool {
	if val&0x80000000 != 0 { // this version only handles 31 bits
		return false
	}
	ok := true
	switch {
	case val < 0x80:
		return bw.writeRawUint32NoCheck(val, 8)
	case val < 0x800:
		ok = bw.writeRawUint32NoCheck(0xC0|(val>>6), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|(val&0x3F), 8) && ok
	case val < 0x10000:
		ok = bw.writeRawUint32NoCheck(0xE0|(val>>12), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|((val>>6)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|(val&0x3F), 8) && ok
	case val < 0x200000:
		ok = bw.writeRawUint32NoCheck(0xF0|(val>>18), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|((val>>12)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|((val>>6)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|(val&0x3F), 8) && ok
	case val < 0x4000000:
		ok = bw.writeRawUint32NoCheck(0xF8|(val>>24), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|((val>>18)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|((val>>12)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|((val>>6)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|(val&0x3F), 8) && ok
	default:
		ok = bw.writeRawUint32NoCheck(0xFC|(val>>30), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|((val>>24)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|((val>>18)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|((val>>12)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|((val>>6)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|(val&0x3F), 8) && ok
	}
	return ok
}

// WriteUTF8Uint64 — port of FLAC__bitwriter_write_utf8_uint64
// (bitwriter.c:876). Handles 36-bit values.
func (bw *BitWriter) WriteUTF8Uint64(val uint64) bool {
	if val&0xFFFFFFF000000000 != 0 { // this version only handles 36 bits
		return false
	}
	ok := true
	switch {
	case val < 0x80:
		return bw.writeRawUint32NoCheck(uint32(val), 8)
	case val < 0x800:
		ok = bw.writeRawUint32NoCheck(0xC0|uint32(val>>6), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32(val&0x3F), 8) && ok
	case val < 0x10000:
		ok = bw.writeRawUint32NoCheck(0xE0|uint32(val>>12), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>6)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32(val&0x3F), 8) && ok
	case val < 0x200000:
		ok = bw.writeRawUint32NoCheck(0xF0|uint32(val>>18), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>12)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>6)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32(val&0x3F), 8) && ok
	case val < 0x4000000:
		ok = bw.writeRawUint32NoCheck(0xF8|uint32(val>>24), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>18)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>12)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>6)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32(val&0x3F), 8) && ok
	case val < 0x80000000:
		ok = bw.writeRawUint32NoCheck(0xFC|uint32(val>>30), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>24)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>18)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>12)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>6)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32(val&0x3F), 8) && ok
	default:
		ok = bw.writeRawUint32NoCheck(0xFE, 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>30)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>24)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>18)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>12)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32((val>>6)&0x3F), 8) && ok
		ok = bw.writeRawUint32NoCheck(0x80|uint32(val&0x3F), 8) && ok
	}
	return ok
}

// ZeroPadToByteBoundary — port of
// FLAC__bitwriter_zero_pad_to_byte_boundary (bitwriter.c:932).
func (bw *BitWriter) ZeroPadToByteBoundary() bool {
	if bw.bits&7 != 0 {
		return bw.WriteZeroes(8 - (bw.bits & 7))
	}
	return true
}

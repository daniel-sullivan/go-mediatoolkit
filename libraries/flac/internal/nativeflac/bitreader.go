package nativeflac

import (
	"encoding/binary"
	"math/bits"
)

// 1:1 port of libflac/src/libFLAC/bitreader.c.
//
// Bit-level reader over a callback-supplied byte stream. This is the
// word-accumulator design libFLAC uses (bitreader.c:103): the input is
// buffered as a slice of big-endian-decoded uint64 "words" so reads can
// shift+mask out of a register-resident word and the CRC-16 fold can
// iterate 8 bytes per loop via CRC16UpdateWords64.
//
// Buffer layout (matching the C struct comments at bitreader.c:104–123):
//   - buffer[i] holds 8 stream bytes, decoded big-endian to host order,
//     so the FIRST stream byte of a word sits in its most-significant
//     byte. MSB-first bit extraction is therefore "shift down from the
//     top of the word".
//   - words is the count of COMPLETE words in buffer.
//   - bytes is the count of stream bytes in the partial tail word
//     buffer[words] (0..7). That partial word is LEFT-justified (its
//     valid bytes occupy the high `bytes*8` bits), which is what lets a
//     read straddle from a complete word into the tail uniformly.
//   - consumedWords + consumedBits is the read cursor: consumedWords
//     fully-consumed words plus consumedBits (0..63) bits already taken
//     from the head of buffer[consumedWords].
//
// CRC-16 is tracked over the bytes the caller asked to CRC. crc16Offset
// is the word index up to which buffer has NOT been folded; crc16Align
// is the bit offset inside the head word where the unfolded region
// begins (always a multiple of 8). crc16_update_block_ folds complete
// words via CRC16UpdateWords64.

const bitsPerWord = 64
const bytesPerWord = 8

// BitReaderReadCallback is the read callback signature: it asks the
// caller to fill up to len(buf) bytes and reports the count actually
// read. Returning false signals an unrecoverable read error.
//
// Mirrors libFLAC's typedef
//
//	FLAC__bool (*FLAC__BitReaderReadCallback)(FLAC__byte buf[], size_t *bytes, void *cd)
type BitReaderReadCallback func(buf []byte) (n uint, ok bool)

const (
	// bitreaderDefaultCapacityWords is the initial buffer size in words.
	// libFLAC's FLAC__BITREADER_DEFAULT_CAPACITY (bitreader.c:101) is
	// 65536 / FLAC__BITS_PER_WORD = 1024 words on a 64-bit build.
	bitreaderDefaultCapacityWords = 65536 / bitsPerWord

	// invalidLimit is the sentinel libFLAC uses for "no limit": -1 in
	// 32-bit unsigned, propagated as 0xFFFFFFFF.
	invalidLimit = ^uint32(0)
)

// BitReader is the Go counterpart of FLAC__BitReader (bitreader.c:103).
type BitReader struct {
	buffer []uint64 // working buffer; cap is the word capacity

	words int // # of completed words in buffer
	bytes int // # of stream bytes in the incomplete tail word buffer[words]

	consumedWords int // # whole words consumed from the front
	consumedBits  int // # bits of buffer[consumedWords] already consumed (0..63)

	// readCRC16 tracks a running CRC-16 over the bytes the caller has
	// indicated should be CRC'd. crc16Offset is the word index up to
	// which we have NOT folded yet; crc16Align is the bit offset inside
	// that head word where the unfolded region begins.
	readCRC16   uint16
	crc16Offset int
	crc16Align  int

	// readLimitSet enables the bit-counted read limit. While set,
	// readLimit holds the remaining bit budget; reads beyond it
	// invalidate the reader.
	readLimitSet bool
	readLimit    uint32

	// lastSeenFramesync stores the bit offset of the most recent
	// framesync from the front of the buffer, or -1 if none/invalidated.
	lastSeenFramesync int64

	readCallback BitReaderReadCallback
}

// NewBitReader — port of FLAC__bitreader_new (bitreader.c:256).
func NewBitReader() *BitReader { return &BitReader{lastSeenFramesync: -1} }

// Init — port of FLAC__bitreader_init (bitreader.c:286). Reserves the
// default-sized buffer and stashes the read callback.
func (br *BitReader) Init(rcb BitReaderReadCallback) bool {
	br.words = 0
	br.bytes = 0
	br.consumedWords = 0
	br.consumedBits = 0
	// +1 word headroom for the partial tail word that read_from_client
	// writes into past `words`.
	br.buffer = make([]uint64, 0, bitreaderDefaultCapacityWords+1)
	br.readCallback = rcb
	br.readLimitSet = false
	br.readLimit = invalidLimit
	br.lastSeenFramesync = -1
	return true
}

// Free — port of FLAC__bitreader_free (bitreader.c:305). Releases the
// internal buffer; safe to reuse the reader after a fresh Init.
func (br *BitReader) Free() {
	br.buffer = nil
	br.words = 0
	br.bytes = 0
	br.consumedWords = 0
	br.consumedBits = 0
	br.readCallback = nil
	br.readLimitSet = false
	br.readLimit = invalidLimit
	br.lastSeenFramesync = -1
}

// Clear — port of FLAC__bitreader_clear (bitreader.c:322). Drops any
// buffered + consumed state without touching the callback.
func (br *BitReader) Clear() bool {
	br.words = 0
	br.bytes = 0
	br.consumedWords = 0
	br.consumedBits = 0
	br.buffer = br.buffer[:0]
	br.readLimitSet = false
	br.readLimit = invalidLimit
	br.lastSeenFramesync = -1
	return true
}

// SetFramesyncLocation — port of FLAC__bitreader_set_framesync_location
// (bitreader.c:332). Stamps the bit index of the just-read framesync so
// a later Rewind can return to its tail.
func (br *BitReader) SetFramesyncLocation() {
	br.lastSeenFramesync = int64(br.consumedWords)*bitsPerWord + int64(br.consumedBits)
}

// RewindToAfterLastSeenFramesync — port of
// FLAC__bitreader_rewind_to_after_last_seen_framesync
// (bitreader.c:337). Re-positions just past the most recently observed
// framesync. Returns false if there is no recorded location, in which
// case the reader is rewound to its base.
func (br *BitReader) RewindToAfterLastSeenFramesync() bool {
	if br.lastSeenFramesync < 0 {
		br.consumedWords = 0
		br.consumedBits = 0
		return false
	}
	br.consumedWords = int(br.lastSeenFramesync / bitsPerWord)
	br.consumedBits = int(br.lastSeenFramesync % bitsPerWord)
	return true
}

// ResetReadCRC16 — port of FLAC__bitreader_reset_read_crc16
// (bitreader.c:350). Caller must already be byte-aligned (libFLAC
// asserts this).
func (br *BitReader) ResetReadCRC16(seed uint16) {
	br.readCRC16 = seed
	br.crc16Offset = br.consumedWords
	br.crc16Align = br.consumedBits
}

// GetReadCRC16 — port of FLAC__bitreader_get_read_crc16
// (bitreader.c:361). Folds any consumed bytes that have not yet been
// CRC'd into readCRC16 and returns the running CRC.
func (br *BitReader) GetReadCRC16() uint16 {
	// CRC consumed words up to here.
	br.crc16UpdateBlock()
	// CRC any tail bytes in a partially-consumed head word
	// (bitreader.c:373–377). crc16Align is the bit offset within the
	// head word where folding should resume; crc16UpdateBlock left it
	// at 0 (the whole consumed words are folded), so fold the consumed
	// whole bytes of the head word from crc16Align up to consumedBits.
	if (br.consumedBits&7) == 0 && br.consumedBits != 0 {
		tail := br.buffer[br.consumedWords]
		for br.crc16Align < br.consumedBits {
			shift := uint(bitsPerWord - 8 - br.crc16Align)
			b := byte(tail >> shift)
			br.readCRC16 = (br.readCRC16 << 8) ^ crc16Table[0][byte(br.readCRC16>>8)^b]
			br.crc16Align += 8
		}
	}
	return br.readCRC16
}

// crc16UpdateBlock — port of crc16_update_block_ (bitreader.c:131).
// Folds the consumed-but-not-yet-CRC'd complete words (words in
// [crc16Offset, consumedWords)) into readCRC16. If the fold region
// began mid-word (crc16Align != 0) the remaining whole bytes of that
// head word are folded first.
//
// Faithful to the C: crc16Align is only cleared by crc16UpdateWord (when
// a head word is actually folded). When no words have been consumed past
// crc16Offset (reads stayed inside the reset word) crc16Align is left at
// its ResetReadCRC16 value so GetReadCRC16's tail loop skips the
// pre-reset bytes. crc16Offset is set to consumedWords here (an absolute
// word index); read_from_client rebases it to 0 on the buffer shift, and
// repeated GetReadCRC16 calls within the same buffer are idempotent.
func (br *BitReader) crc16UpdateBlock() {
	if br.consumedWords > br.crc16Offset && br.crc16Align != 0 {
		br.crc16UpdateWord(br.buffer[br.crc16Offset]) // also clears crc16Align
		br.crc16Offset++
	}
	if br.consumedWords > br.crc16Offset {
		br.readCRC16 = CRC16UpdateWords64(br.buffer[br.crc16Offset:br.consumedWords], br.readCRC16)
	}
	br.crc16Offset = br.consumedWords
}

// crc16UpdateWord — port of crc16_update_word_ (bitreader.c:122). Folds
// the whole bytes of `word` from crc16Align onward into readCRC16.
func (br *BitReader) crc16UpdateWord(word uint64) {
	crc := br.readCRC16
	for br.crc16Align < bitsPerWord {
		shift := uint(bitsPerWord - 8 - br.crc16Align)
		b := byte(word >> shift)
		crc = (crc << 8) ^ crc16Table[0][byte(crc>>8)^b]
		br.crc16Align += 8
	}
	br.readCRC16 = crc
	br.crc16Align = 0
}

// IsConsumedByteAligned — port of
// FLAC__bitreader_is_consumed_byte_aligned (bitreader.c:381).
func (br *BitReader) IsConsumedByteAligned() bool { return (br.consumedBits & 7) == 0 }

// BitsLeftForByteAlignment — port of
// FLAC__bitreader_bits_left_for_byte_alignment (bitreader.c:386).
func (br *BitReader) BitsLeftForByteAlignment() uint32 { return uint32(8 - (br.consumedBits & 7)) }

// GetInputBitsUnconsumed — port of
// FLAC__bitreader_get_input_bits_unconsumed (bitreader.c:391).
func (br *BitReader) GetInputBitsUnconsumed() uint32 {
	return uint32((br.words-br.consumedWords)*bitsPerWord + br.bytes*8 - br.consumedBits)
}

// SetLimit / RemoveLimit / LimitRemaining / LimitInvalidate —
// bitreader.c:396, 402, 408, 413.
func (br *BitReader) SetLimit(limit uint32) {
	br.readLimit = limit
	br.readLimitSet = true
}
func (br *BitReader) RemoveLimit() {
	br.readLimitSet = false
	br.readLimit = invalidLimit
}
func (br *BitReader) LimitRemaining() uint32 { return br.readLimit }
func (br *BitReader) LimitInvalidate()       { br.readLimit = invalidLimit }

// readFromClient — port of bitreader_read_from_client_
// (bitreader.c:157). Compacts the buffer (drops already-consumed
// words), folds them into the CRC, then asks the read callback to top
// us up. Bytes from the client are appended big-endian into the word
// buffer.
func (br *BitReader) readFromClient() bool {
	// First shift the unconsumed buffer data toward the front.
	if br.consumedWords > 0 {
		br.lastSeenFramesync = -1
		br.crc16UpdateBlock() // CRC consumed words

		start := br.consumedWords
		end := br.words
		if br.bytes != 0 {
			end++
		}
		// Move [start:end] words to the front.
		copy(br.buffer[:end-start], br.buffer[start:end])
		br.words -= start
		br.consumedWords = 0
		// crc16Offset was set to consumedWords (== start) by
		// crc16UpdateBlock; after the shift it rebases to 0.
		br.crc16Offset = 0
	}

	// Compute how many free bytes we can read into. Capacity is in
	// words; the tail partial word lives at buffer[words].
	capWords := cap(br.buffer)
	freeBytes := (capWords-br.words)*bytesPerWord - br.bytes
	if freeBytes == 0 {
		return false
	}

	// Ensure the slice exposes the tail word so we can write into it.
	// We assemble the read into a scratch byte view, then re-pack into
	// words. To avoid per-call allocation we read directly into a stack
	// buffer sized to freeBytes-capped chunk.
	//
	// Lay out the current partial tail bytes (left-justified in the tail
	// word) followed by the freshly read bytes, then re-pack whole
	// words big-endian.
	//
	// Grow the slice length to expose the tail word index for packing.
	need := br.words
	if br.bytes != 0 {
		need = br.words + 1
	}
	if len(br.buffer) < need {
		br.buffer = br.buffer[:need]
	}

	// Read into a temporary byte buffer.
	tmp := make([]byte, freeBytes)
	n, ok := br.readCallback(tmp)
	if !ok {
		return false
	}
	if n == 0 {
		return true
	}

	// Total stream bytes now sitting past the last complete word.
	// Existing partial tail bytes come from buffer[words] (left
	// justified), then the new bytes.
	totalTailBytes := br.bytes + int(n)

	// Extract the existing partial tail bytes from the tail word.
	var tail [bytesPerWord]byte
	if br.bytes != 0 {
		binary.BigEndian.PutUint64(tail[:], br.buffer[br.words])
	}

	// Re-pack: word index `w` starting at br.words, consuming bytes from
	// `tail[:br.bytes]` then from tmp[:n].
	w := br.words
	srcByte := func(i int) byte {
		if i < br.bytes {
			return tail[i]
		}
		return tmp[i-br.bytes]
	}
	full := totalTailBytes / bytesPerWord
	rem := totalTailBytes % bytesPerWord
	wantLen := w + full
	if rem != 0 {
		wantLen++
	}
	if len(br.buffer) < wantLen {
		br.buffer = br.buffer[:wantLen]
	}
	for k := 0; k < full; k++ {
		base := k * bytesPerWord
		br.buffer[w+k] = uint64(srcByte(base))<<56 | uint64(srcByte(base+1))<<48 |
			uint64(srcByte(base+2))<<40 | uint64(srcByte(base+3))<<32 |
			uint64(srcByte(base+4))<<24 | uint64(srcByte(base+5))<<16 |
			uint64(srcByte(base+6))<<8 | uint64(srcByte(base+7))
	}
	// Partial tail word, left-justified.
	if rem != 0 {
		var v uint64
		base := full * bytesPerWord
		for j := 0; j < rem; j++ {
			v |= uint64(srcByte(base+j)) << uint(bitsPerWord-8-j*8)
		}
		br.buffer[w+full] = v
	}
	br.words = w + full
	br.bytes = rem
	return true
}

// ensureBits pulls bytes from the client until the buffer holds at least
// `bits` more bits past the current position.
func (br *BitReader) ensureBits(bits uint32) bool {
	for br.GetInputBitsUnconsumed() < bits {
		if !br.readFromClient() {
			return false
		}
	}
	return true
}

// ReadRawUint32 — port of FLAC__bitreader_read_raw_uint32
// (bitreader.c:418). Reads `bits` bits (0..32) MSB-first into val.
func (br *BitReader) ReadRawUint32(nbits uint32) (val uint32, ok bool) {
	if nbits == 0 {
		return 0, true
	}
	if br.readLimitSet && br.readLimit != invalidLimit {
		if br.readLimit < nbits {
			br.readLimit = invalidLimit
			return 0, false
		}
		br.readLimit -= nbits
	}
	if !br.ensureBits(nbits) {
		return 0, false
	}

	bitsLeft := nbits
	if br.consumedWords < br.words {
		if br.consumedBits != 0 {
			n := uint32(bitsPerWord - br.consumedBits)
			word := br.buffer[br.consumedWords]
			mask := allOnes64 >> uint(br.consumedBits)
			if bitsLeft < n {
				shift := n - bitsLeft
				val = uint32((word & mask) >> uint(shift))
				br.consumedBits += int(bitsLeft)
				return val, true
			}
			val = uint32(word & mask)
			bitsLeft -= n
			br.consumedWords++
			br.consumedBits = 0
			if bitsLeft != 0 {
				shift := bitsPerWord - bitsLeft
				val = val << uint(bitsLeft)
				val |= uint32(br.buffer[br.consumedWords] >> uint(shift))
				br.consumedBits = int(bitsLeft)
			}
			return val, true
		}
		// consumedBits == 0
		word := br.buffer[br.consumedWords]
		if bitsLeft < bitsPerWord {
			val = uint32(word >> uint(bitsPerWord-bitsLeft))
			br.consumedBits = int(bitsLeft)
			return val, true
		}
		// bitsLeft == 64 impossible (bits<=32); here bits == word size
		// only when bitsPerWord==32, not on this 64-bit port. With
		// nbits<=32 < 64 this branch is unreachable, but keep it safe:
		val = uint32(word)
		br.consumedWords++
		return val, true
	}
	// Starting our read at a partial tail word; ensureBits guaranteed
	// at least `bits` bits are available.
	if br.consumedBits != 0 {
		val = uint32((br.buffer[br.consumedWords] & (allOnes64 >> uint(br.consumedBits))) >> uint(bitsPerWord-br.consumedBits-int(bitsLeft)))
		br.consumedBits += int(bitsLeft)
		return val, true
	}
	val = uint32(br.buffer[br.consumedWords] >> uint(bitsPerWord-int(bitsLeft)))
	br.consumedBits += int(bitsLeft)
	return val, true
}

const allOnes64 = ^uint64(0)

// updateCRC16 folds bytes into a running CRC-16 starting from `crc`,
// replaying libFLAC's FLAC__CRC16_UPDATE byte loop. Used by CRC16Seed
// (channel.go) to fold the two frame-header warmup bytes.
func updateCRC16(data []byte, crc uint16) uint16 {
	for _, b := range data {
		crc = (crc << 8) ^ crc16Table[0][byte(crc>>8)^b]
	}
	return crc
}

// ReadRawInt32 — port of FLAC__bitreader_read_raw_int32
// (bitreader.c:508). Sign-extends a `bits`-bit unsigned read.
func (br *BitReader) ReadRawInt32(bits uint32) (val int32, ok bool) {
	if bits < 1 {
		return 0, false
	}
	uval, ok := br.ReadRawUint32(bits)
	if !ok {
		return 0, false
	}
	var mask uint32
	if bits >= 33 {
		mask = 0
	} else {
		mask = uint32(1) << (bits - 1)
	}
	return int32(uval^mask) - int32(mask), true
}

// ReadRawUint64 — port of FLAC__bitreader_read_raw_uint64
// (bitreader.c:521).
func (br *BitReader) ReadRawUint64(nbits uint32) (val uint64, ok bool) {
	if nbits > 32 {
		hi, ok := br.ReadRawUint32(nbits - 32)
		if !ok {
			return 0, false
		}
		lo, ok := br.ReadRawUint32(32)
		if !ok {
			return 0, false
		}
		return (uint64(hi) << 32) | uint64(lo), true
	}
	lo, ok := br.ReadRawUint32(nbits)
	if !ok {
		return 0, false
	}
	return uint64(lo), true
}

// ReadRawInt64 — port of FLAC__bitreader_read_raw_int64
// (bitreader.c:542).
func (br *BitReader) ReadRawInt64(nbits uint32) (val int64, ok bool) {
	if nbits < 1 {
		return 0, false
	}
	uval, ok := br.ReadRawUint64(nbits)
	if !ok {
		return 0, false
	}
	var mask uint64
	if nbits >= 65 {
		mask = 0
	} else {
		mask = uint64(1) << (nbits - 1)
	}
	return int64(uval^mask) - int64(mask), true
}

// ReadUint32LittleEndian — port of
// FLAC__bitreader_read_uint32_little_endian (bitreader.c:555). Reads
// four bytes and assembles them LE-first; used by VORBIS_COMMENT.
func (br *BitReader) ReadUint32LittleEndian() (val uint32, ok bool) {
	b0, ok := br.ReadRawUint32(8)
	if !ok {
		return 0, false
	}
	b1, ok := br.ReadRawUint32(8)
	if !ok {
		return 0, false
	}
	b2, ok := br.ReadRawUint32(8)
	if !ok {
		return 0, false
	}
	b3, ok := br.ReadRawUint32(8)
	if !ok {
		return 0, false
	}
	return b0 | (b1 << 8) | (b2 << 16) | (b3 << 24), true
}

// SkipBitsNoCRC — port of FLAC__bitreader_skip_bits_no_crc
// (bitreader.c:580). Skips `bits` bits without folding them into the
// CRC.
func (br *BitReader) SkipBitsNoCRC(nbits uint32) bool {
	if nbits == 0 {
		return true
	}
	n := uint32(br.consumedBits & 7)
	if n != 0 {
		m := uint32(8) - n
		if m > nbits {
			m = nbits
		}
		if _, ok := br.ReadRawUint32(m); !ok {
			return false
		}
		nbits -= m
	}
	m := nbits / 8
	if m > 0 {
		if !br.SkipByteBlockAlignedNoCRC(m) {
			return false
		}
		nbits %= 8
	}
	if nbits > 0 {
		if _, ok := br.ReadRawUint32(nbits); !ok {
			return false
		}
	}
	return true
}

// SkipByteBlockAlignedNoCRC — port of
// FLAC__bitreader_skip_byte_block_aligned_no_crc (bitreader.c:615).
// Caller must be byte-aligned (libFLAC asserts).
func (br *BitReader) SkipByteBlockAlignedNoCRC(nvals uint32) bool {
	if br.readLimitSet && br.readLimit != invalidLimit {
		if br.readLimit < nvals*8 {
			br.readLimit = invalidLimit
			return false
		}
	}
	// step 1: skip over partial head word to get word aligned.
	for nvals != 0 && br.consumedBits != 0 {
		if _, ok := br.ReadRawUint32(8); !ok {
			return false
		}
		nvals--
	}
	if nvals == 0 {
		return true
	}
	// step 2: skip whole words in chunks.
	for nvals >= bytesPerWord {
		if br.consumedWords < br.words {
			br.consumedWords++
			nvals -= bytesPerWord
			if br.readLimitSet {
				br.readLimit -= bitsPerWord
			}
		} else if !br.readFromClient() {
			return false
		}
	}
	// step 3: skip any remainder from partial tail bytes.
	for nvals != 0 {
		if _, ok := br.ReadRawUint32(8); !ok {
			return false
		}
		nvals--
	}
	return true
}

// ReadByteBlockAlignedNoCRC — port of
// FLAC__bitreader_read_byte_block_aligned_no_crc (bitreader.c:660).
// Caller must be byte-aligned.
func (br *BitReader) ReadByteBlockAlignedNoCRC(out []byte) bool {
	nvals := uint32(len(out))
	if br.readLimitSet && br.readLimit != invalidLimit {
		if br.readLimit < nvals*8 {
			br.readLimit = invalidLimit
			return false
		}
	}
	idx := 0
	// step 1: read from partial head word to get word aligned.
	for nvals != 0 && br.consumedBits != 0 {
		x, ok := br.ReadRawUint32(8)
		if !ok {
			return false
		}
		out[idx] = byte(x)
		idx++
		nvals--
	}
	if nvals == 0 {
		return true
	}
	// step 2: read whole words in chunks.
	for nvals >= bytesPerWord {
		if br.consumedWords < br.words {
			word := br.buffer[br.consumedWords]
			br.consumedWords++
			binary.BigEndian.PutUint64(out[idx:idx+8], word)
			idx += bytesPerWord
			nvals -= bytesPerWord
			if br.readLimitSet {
				br.readLimit -= bitsPerWord
			}
		} else if !br.readFromClient() {
			return false
		}
	}
	// step 3: read any remainder from partial tail bytes.
	for nvals != 0 {
		x, ok := br.ReadRawUint32(8)
		if !ok {
			return false
		}
		out[idx] = byte(x)
		idx++
		nvals--
	}
	return true
}

// ReadUnaryUnsigned — port of FLAC__bitreader_read_unary_unsigned
// (bitreader.c:725, fast version). Returns the count of zero bits before
// the next 1 bit (and consumes the 1 bit), counting MSB-first zeros
// word-at-a-time via bits.LeadingZeros64.
func (br *BitReader) ReadUnaryUnsigned() (val uint32, ok bool) {
	val = 0
	for {
		for br.consumedWords < br.words {
			var b uint64
			if br.consumedBits < bitsPerWord {
				b = br.buffer[br.consumedWords] << uint(br.consumedBits)
			}
			if b != 0 {
				i := uint32(bits.LeadingZeros64(b))
				val += i
				i++
				br.consumedBits += int(i)
				if br.consumedBits >= bitsPerWord {
					br.consumedWords++
					br.consumedBits = 0
				}
				return val, true
			}
			val += uint32(bitsPerWord - br.consumedBits)
			br.consumedWords++
			br.consumedBits = 0
		}
		// Try reading through any tail bytes before the read callback.
		if br.bytes*8 > br.consumedBits {
			end := br.bytes * 8
			var b uint64
			b = (br.buffer[br.consumedWords] & (allOnes64 << uint(bitsPerWord-end))) << uint(br.consumedBits)
			if b != 0 {
				i := uint32(bits.LeadingZeros64(b))
				val += i
				i++
				br.consumedBits += int(i)
				return val, true
			}
			val += uint32(end - br.consumedBits)
			br.consumedBits = end
		}
		if !br.readFromClient() {
			return 0, false
		}
	}
}

// ReadRiceSignedBlock — port of FLAC__bitreader_read_rice_signed_block
// (deduplication/bitreader_read_rice_signed_block.c). Word-accumulator
// fast path: reads nvals zigzag-encoded Rice values, each a unary MSB
// count followed by `parameter` LSB bits, never straddling more than two
// words for the binary part (guaranteed by parameter < 32, word >= 32
// bits).
func (br *BitReader) ReadRiceSignedBlock(vals []int32, parameter uint32) bool {
	if parameter >= 32 {
		return false
	}
	limit := invalidLimit >> parameter

	idx := 0
	n := len(vals)

	if parameter == 0 {
		for idx < n {
			msbs, ok := br.ReadUnaryUnsigned()
			if !ok {
				return false
			}
			vals[idx] = int32(msbs>>1) ^ -int32(msbs&1)
			idx++
		}
		return true
	}

	cwords := br.consumedWords
	words := br.words
	var ucbits uint32 // unconsumed bits in head word
	var b uint64
	var x uint32

	// The C uses goto into a do/while tail block (with incomplete_msbs /
	// incomplete_lsbs entry points) which Go forbids. We model the three
	// entry conditions with a small state machine:
	//   tailEntry == 0 → fresh tail iteration (read unary, then full LSBs)
	//   tailEntry == 1 → "incomplete_msbs": partial msbs in x, ucbits=0
	//   tailEntry == 2 → "incomplete_lsbs": msbs known (carriedMsbs),
	//                     partial lsbs in x, ucbits valid LSB bits in x
	// inTail drives whether we run the fast word path or the tail path.
	inTail := cwords >= words
	tailEntry := 0
	var carriedMsbs uint32

	if !inTail {
		ucbits = uint32(bitsPerWord - br.consumedBits)
		b = br.buffer[cwords] << uint(br.consumedBits)
	} else {
		x = 0
	}

	for idx < n {
		if !inTail {
			// --- fast word path ---
			// read the unary MSBs and end bit
			y := uint32(bits.LeadingZeros64(b))
			x = y
			if x == bitsPerWord {
				x = ucbits
				for {
					cwords++
					if cwords >= words {
						// incomplete_msbs
						br.consumedBits = 0
						br.consumedWords = cwords
						inTail = true
						tailEntry = 1
						break
					}
					b = br.buffer[cwords]
					y = uint32(bits.LeadingZeros64(b))
					x += y
					if y != bitsPerWord {
						break
					}
				}
				if inTail {
					continue // jump to tail handling
				}
			}
			b <<= uint(y)
			b <<= 1 // account for stop bit
			ucbits = (ucbits - x - 1) % bitsPerWord
			msbs := x

			if x > limit {
				return false
			}

			// read the binary LSBs
			x = uint32(b >> uint(bitsPerWord-parameter))
			if parameter <= ucbits {
				ucbits -= parameter
				b <<= parameter
			} else {
				cwords++
				if cwords >= words {
					// incomplete_lsbs
					br.consumedBits = 0
					br.consumedWords = cwords
					inTail = true
					tailEntry = 2
					carriedMsbs = msbs
					continue
				}
				b = br.buffer[cwords]
				ucbits += uint32(bitsPerWord) - parameter
				x |= uint32(b >> uint(ucbits))
				b <<= uint(uint32(bitsPerWord) - ucbits)
			}
			lsbs := x

			x = (msbs << parameter) | lsbs
			vals[idx] = int32(x>>1) ^ -int32(x&1)
			idx++
			continue
		}

		// --- tail path (one value per iteration) ---
		var msbs uint32
		switch tailEntry {
		case 2:
			// incomplete_lsbs: msbs known, partial lsbs in x with
			// `ucbits` valid bits already present.
			msbs = carriedMsbs
		default:
			// fresh (0) or incomplete_msbs (1): read unary, fold partial.
			m, ok := br.ReadUnaryUnsigned()
			if !ok {
				return false
			}
			msbs = m + x
			x = 0
			ucbits = 0
		}

		lsbs, ok := br.ReadRawUint32(parameter - ucbits)
		if !ok {
			return false
		}
		lsbs = x | lsbs

		xx := (msbs << parameter) | lsbs
		vals[idx] = int32(xx>>1) ^ -int32(xx&1)
		idx++
		x = 0

		cwords = br.consumedWords
		words = br.words
		ucbits = uint32(bitsPerWord - br.consumedBits)
		if cwords < cap(br.buffer) {
			b = br.buffer[cwords] << uint(br.consumedBits)
		} else {
			b = 0
		}

		// Stay in the tail only while we still have no whole words.
		if cwords >= words && idx < n {
			tailEntry = 0
			continue
		}
		inTail = false
		tailEntry = 0
	}

	if ucbits == 0 && cwords < words {
		cwords++
		ucbits = bitsPerWord
	}

	br.consumedBits = bitsPerWord - int(ucbits)
	br.consumedWords = cwords
	return true
}

// ReadUTF8Uint32 — port of FLAC__bitreader_read_utf8_uint32
// (bitreader.c:928). On an invalid sequence the function returns
// (0xFFFFFFFF, true) — note the success bool: libFLAC reserves `false`
// for read failure (no more bytes), not for malformed content. The
// optional raw buffer captures the consumed bytes.
func (br *BitReader) ReadUTF8Uint32(raw []byte) (val uint32, rawLen int, ok bool) {
	x, ok := br.ReadRawUint32(8)
	if !ok {
		return 0, 0, false
	}
	if raw != nil {
		raw[rawLen] = byte(x)
	}
	rawLen = 1
	var v, i uint32
	switch {
	case x&0x80 == 0:
		v = x
	case x&0xE0 == 0xC0:
		v = x & 0x1F
		i = 1
	case x&0xF0 == 0xE0:
		v = x & 0x0F
		i = 2
	case x&0xF8 == 0xF0:
		v = x & 0x07
		i = 3
	case x&0xFC == 0xF8:
		v = x & 0x03
		i = 4
	case x&0xFE == 0xFC:
		v = x & 0x01
		i = 5
	default:
		return 0xFFFFFFFF, rawLen, true
	}
	for ; i > 0; i-- {
		x, ok = br.ReadRawUint32(8)
		if !ok {
			return 0, rawLen, false
		}
		if raw != nil {
			raw[rawLen] = byte(x)
		}
		rawLen++
		if x&0x80 == 0 || x&0x40 != 0 {
			return 0xFFFFFFFF, rawLen, true
		}
		v = (v << 6) | (x & 0x3F)
	}
	return v, rawLen, true
}

// ReadUTF8Uint64 — port of FLAC__bitreader_read_utf8_uint64
// (bitreader.c:983). Like ReadUTF8Uint32 but accepts the 7-byte
// 0xFE-prefix encoding used by FLAC sample numbers.
func (br *BitReader) ReadUTF8Uint64(raw []byte) (val uint64, rawLen int, ok bool) {
	x, ok := br.ReadRawUint32(8)
	if !ok {
		return 0, 0, false
	}
	if raw != nil {
		raw[rawLen] = byte(x)
	}
	rawLen = 1
	var v uint64
	var i uint32
	switch {
	case x&0x80 == 0:
		v = uint64(x)
	case x&0xE0 == 0xC0:
		v = uint64(x & 0x1F)
		i = 1
	case x&0xF0 == 0xE0:
		v = uint64(x & 0x0F)
		i = 2
	case x&0xF8 == 0xF0:
		v = uint64(x & 0x07)
		i = 3
	case x&0xFC == 0xF8:
		v = uint64(x & 0x03)
		i = 4
	case x&0xFE == 0xFC:
		v = uint64(x & 0x01)
		i = 5
	case x == 0xFE:
		v = 0
		i = 6
	default:
		return 0xFFFFFFFFFFFFFFFF, rawLen, true
	}
	for ; i > 0; i-- {
		x, ok = br.ReadRawUint32(8)
		if !ok {
			return 0, rawLen, false
		}
		if raw != nil {
			raw[rawLen] = byte(x)
		}
		rawLen++
		if x&0x80 == 0 || x&0x40 != 0 {
			return 0xFFFFFFFFFFFFFFFF, rawLen, true
		}
		v = (v << 6) | uint64(x&0x3F)
	}
	return v, rawLen, true
}

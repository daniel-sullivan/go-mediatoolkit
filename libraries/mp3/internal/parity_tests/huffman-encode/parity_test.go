// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package huffmanencode

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// These parity tests pin the pure-Go nativemp3 huffman-encode bit writer
// (bitstream_encode.go: PutBits2 / PutBitsNoHeaders / putheaderBits, ported
// 1:1 from LAME 3.100 bitstream.c) against the vendored LAME C reference
// (huffman_encode_cgo_src.c). Each routine is driven on both sides over
// identical fabricated input and the full observable state — running bit
// count, byte/bit cursors, header write pointer, and the produced bytes — must
// be bit-for-bit equal.
//
// The slice is integer-only — pure shifting, no floating point — so its
// results are independent of FMA/vectorization. The bit-exact assertions are
// nonetheless gated behind nativemp3.StrictMode per the FP-parity convention,
// so a bare `go test` is clean and the strict run (mp3_strict + the FP CGO
// env) is the authoritative bit-exact gate.
//
// NOTE: the Huffman code emitters that share this slice (Huffmancode /
// huffman_coder_count1 / Short/LongHuffmancodebits) are NOT pinned here. The
// Go ht[] codebook array is declared but still empty (huffman_encode.go:51)
// and the emitter methods are unexported, so there is nothing to compare yet.
// Once the tables.c port populates ht[] and the emitters are exported, add
// emitter trampolines to huffman_encode_cgo_src.c and assertions here.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("bit-exact parity asserted only under -tags=mp3_strict (FP env via mise //libraries/mp3:parity)")
	}
}

// disarmSentinel is a write_timing value the running totbit can never reach in
// these tests (the test sequences emit far fewer than this many bits), so an
// unprimed header slot never triggers a spurious splice.
const disarmSentinel = 1 << 30

// emitOp is one bit-writer call in a fabricated sequence.
type emitOp struct {
	val int
	j   int
}

// randomOps builds a sequence of (val, j) emit ops. j stays in [1,24] (LAME's
// putbits2 asserts j < MAX_LENGTH-2 == 30; the real encoder never writes more
// than 24 bits at once) and val is masked to its low j bits — exactly the
// width the writer consumes — so the comparison covers full bytes, partial
// bytes, and byte-boundary crossings.
func randomOps(rng *rand.Rand, n int) []emitOp {
	ops := make([]emitOp, n)
	for i := range ops {
		j := 1 + rng.IntN(24)
		var mask int
		if j >= 31 {
			mask = -1
		} else {
			mask = (1 << uint(j)) - 1
		}
		ops[i] = emitOp{val: int(rng.Uint32()) & mask, j: j}
	}
	return ops
}

// assertState compares every observable field of the two writers plus the
// output bytes up to the byte currently being filled (inclusive).
func assertState(t *testing.T, c *cgoEnc, n *nativeEnc) {
	t.Helper()
	assert.Equal(t, c.totbit(), n.totbit(), "totbit")
	assert.Equal(t, c.bufByteIdx(), n.bufByteIdx(), "buf_byte_idx")
	assert.Equal(t, c.bufBitIdx(), n.bufBitIdx(), "buf_bit_idx")
	assert.Equal(t, c.wPtr(), n.wPtr(), "w_ptr")
	// Compare the populated portion of the output buffer (byte 0..buf_byte_idx).
	nbytes := c.bufByteIdx() + 1
	assert.Equal(t, c.bytes(nbytes), n.bytes(nbytes), "output bytes")
}

func TestPutBits2Parity(t *testing.T) {
	requireStrict(t)
	const bufSize = 1 << 16
	for seed := uint64(0); seed < 64; seed++ {
		rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
		ops := randomOps(rng, 200)

		c := newCgoEnc(bufSize)
		c.disarmHeaders(disarmSentinel)
		n := newNativeEnc(bufSize)
		n.disarmHeaders(disarmSentinel)

		for _, op := range ops {
			c.putBits2(op.val, op.j)
			n.putBits2(op.val, op.j)
		}
		assertState(t, c, n)
		c.free()
	}
}

func TestPutBitsNoHeadersParity(t *testing.T) {
	requireStrict(t)
	const bufSize = 1 << 16
	for seed := uint64(0); seed < 64; seed++ {
		rng := rand.New(rand.NewPCG(seed, 0xc2b2ae3d27d4eb4f))
		ops := randomOps(rng, 200)

		c := newCgoEnc(bufSize)
		n := newNativeEnc(bufSize)

		for _, op := range ops {
			c.putBitsNoHeaders(op.val, op.j)
			n.putBitsNoHeaders(op.val, op.j)
		}
		assertState(t, c, n)
		c.free()
	}
}

// TestPutBits2EdgeWidths drives the extreme bit widths (1 and 24) and exact
// byte-aligned widths (8, 16) interleaved, so the Min(j, buf_bit_idx)
// splitting and the buf_bit_idx==0 byte advance are exercised at their
// boundaries.
func TestPutBits2EdgeWidths(t *testing.T) {
	requireStrict(t)
	const bufSize = 1 << 12
	widths := []int{1, 8, 1, 16, 24, 1, 7, 9, 8, 24, 1, 1, 1, 1, 1, 1, 1, 1, 13}
	c := newCgoEnc(bufSize)
	c.disarmHeaders(disarmSentinel)
	n := newNativeEnc(bufSize)
	n.disarmHeaders(disarmSentinel)
	for i, j := range widths {
		val := (0xA5A5A5 ^ (i * 0x3B9)) & ((1 << uint(j)) - 1)
		c.putBits2(val, j)
		n.putBits2(val, j)
		assertState(t, c, n)
	}
	c.free()
}

// TestHeaderSpliceParity primes a header slot's write_timing so that PutBits2,
// on crossing that totbit at a byte boundary, splices the buffered side-info
// header (putheader_bits) into the stream. It verifies both sides splice the
// identical bytes and advance w_ptr identically.
func TestHeaderSpliceParity(t *testing.T) {
	requireStrict(t)
	const bufSize = 1 << 14

	// Emit a whole number of bytes first, then prime the next byte boundary's
	// totbit as a header write_timing. putbits2 checks the splice at the START
	// of a new byte (buf_bit_idx==0), i.e. when totbit is a multiple of 8.
	for _, sideinfoLen := range []int{17, 32} {
		header := make([]byte, sideinfoLen)
		for i := range header {
			header[i] = byte(0x40 + i)
		}

		// Pre-emit preBytes bytes (preBytes*8 bits) before the splice point.
		const preBytes = 5
		writeTiming := preBytes * 8

		c := newCgoEnc(bufSize)
		c.disarmHeaders(disarmSentinel)
		c.setSideinfoLen(sideinfoLen)
		c.setWPtr(0)
		c.primeHeader(0, writeTiming, header)

		n := newNativeEnc(bufSize)
		n.disarmHeaders(disarmSentinel)
		n.setSideinfoLen(sideinfoLen)
		n.setWPtr(0)
		n.primeHeader(0, writeTiming, header)

		// Emit preBytes full bytes, then more data that crosses the boundary.
		for i := 0; i < preBytes; i++ {
			c.putBits2(0x5A, 8)
			n.putBits2(0x5A, 8)
		}
		// This next write opens a new byte at totbit==writeTiming → splice.
		c.putBits2(0x1234, 16)
		n.putBits2(0x1234, 16)

		assertState(t, c, n)
		assert.Equal(t, 1, c.wPtr(), "C w_ptr advanced past spliced header (sideinfoLen=%d)", sideinfoLen)
		require.Equal(t, c.wPtr(), n.wPtr(), "w_ptr after splice (sideinfoLen=%d)", sideinfoLen)
		c.free()
	}
}

// TestPutHeaderBitsParity pins the putheader_bits splice op in isolation: it
// primes a header slot, advances buf_byte_idx by emitting bytes, then calls
// putheader_bits directly and checks the spliced bytes, the advanced cursor,
// the bit count, and w_ptr match.
func TestPutHeaderBitsParity(t *testing.T) {
	requireStrict(t)
	const bufSize = 1 << 14
	sideinfoLen := 23
	header := make([]byte, sideinfoLen)
	for i := range header {
		header[i] = byte(0x80 ^ (i * 7))
	}

	c := newCgoEnc(bufSize)
	c.disarmHeaders(disarmSentinel)
	c.setSideinfoLen(sideinfoLen)
	c.setWPtr(3)
	c.primeHeader(3, disarmSentinel, header)

	n := newNativeEnc(bufSize)
	n.disarmHeaders(disarmSentinel)
	n.setSideinfoLen(sideinfoLen)
	n.setWPtr(3)
	n.primeHeader(3, disarmSentinel, header)

	// Emit a few bytes so buf_byte_idx > 0, then splice directly.
	for i := 0; i < 4; i++ {
		c.putBitsNoHeaders(0x33, 8)
		n.putBitsNoHeaders(0x33, 8)
	}
	c.putHeaderBits()
	n.putHeaderBits()

	assert.Equal(t, c.totbit(), n.totbit(), "totbit after putheader_bits")
	assert.Equal(t, c.bufByteIdx(), n.bufByteIdx(), "buf_byte_idx after putheader_bits")
	assert.Equal(t, c.wPtr(), n.wPtr(), "w_ptr after putheader_bits")
	assert.Equal(t, 4, c.wPtr(), "C w_ptr advanced from 3 to 4")
	nbytes := c.bufByteIdx() + 1
	assert.Equal(t, c.bytes(nbytes), n.bytes(nbytes), "spliced header bytes")
	c.free()
}

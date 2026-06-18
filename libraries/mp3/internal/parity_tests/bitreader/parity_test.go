//go:build cgo

package bitreader

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// The minimp3 bit reader (bs_t / get_bits) is integer-only, so its output is
// bit-identical regardless of the mp3_strict build tag. These tests still
// gate on nativemp3.StrictMode to keep the suite's invariants uniform with
// the FP-bearing slices (IMDCT, synthesis) and so a bare `go test` stays
// clean while `mise run //libraries/mp3:parity` asserts everything.

// requireStrict skips a parity test unless the mp3_strict tag is set,
// mirroring the FP-bit-exact gate the rest of the suite uses.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("bitreader parity asserts under -tags=mp3_strict (run via mise run //libraries/mp3:parity)")
	}
}

// makeRandomBytes returns n random bytes seeded deterministically so the
// parity sweep is reproducible across runs.
func makeRandomBytes(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, seed+1))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// pair holds the cgo + Go bit readers driven over the same byte stream so the
// differential tests can iterate uniformly.
type pair struct {
	c   *cgoBitReader
	bs  nativemp3.BitStream
	src []byte
}

func newPair(src []byte) *pair {
	p := &pair{c: newCgoBitReader(src), src: append([]byte(nil), src...)}
	nativemp3.BsInit(&p.bs, p.src, len(p.src))
	return p
}

func (p *pair) free() { p.c.free() }

func (p *pair) getBits(n int) (uint32, uint32) {
	return p.c.getBits(n), nativemp3.GetBits(&p.bs, n)
}

// TestParityBsInit pins bs_init: a freshly initialized reader has pos==0 and
// limit==bytes*8 in both the C oracle and the Go port, for a range of lengths
// including the empty stream.
func TestParityBsInit(t *testing.T) {
	requireStrict(t)
	for _, n := range []int{0, 1, 2, 3, 4, 7, 8, 33, 511, 2304} {
		c := newCgoBitReader(makeRandomBytes(uint64(n)+1, n))
		var bs nativemp3.BitStream
		nativemp3.BsInit(&bs, makeRandomBytes(uint64(n)+1, n), n)
		require.Equal(t, c.pos(), bs.Pos, "pos mismatch at len=%d", n)
		require.Equal(t, c.limit(), bs.Limit, "limit mismatch at len=%d", n)
		require.Equal(t, n*8, bs.Limit, "Go limit not bytes*8 at len=%d", n)
		c.free()
	}
}

// TestParityGetBitsRandomWidths sweeps get_bits over random read widths,
// asserting the returned value and the resulting (pos, limit) state stay
// lock-step between the C oracle and the Go port. Widths are capped at 24
// bits: minimp3 never reads wider in a single get_bits call (the largest is
// the 15-bit table field in the side-info parser), and get_bits' 32-bit
// `cache` is only defined for n + (pos&7) <= 32.
func TestParityGetBitsRandomWidths(t *testing.T) {
	requireStrict(t)
	stream := makeRandomBytes(101, 4096)
	r := rand.New(rand.NewPCG(102, 103))
	p := newPair(stream)
	defer p.free()
	for i := 0; i < 5000; i++ {
		n := r.IntN(25) // 0..24
		gotC, gotN := p.getBits(n)
		require.Equal(t, gotC, gotN, "value mismatch at step %d (n=%d)", i, n)
		require.Equal(t, p.c.pos(), p.bs.Pos, "pos mismatch at step %d (n=%d)", i, n)
		require.Equal(t, p.c.limit(), p.bs.Limit, "limit mismatch at step %d (n=%d)", i, n)
	}
}

// TestParityGetBitsFixedWidths exercises each get_bits width minimp3 actually
// uses (the n-literals threaded through bs_init/side-info/Huffman: 1, 2, 3,
// 4, 5, 7, 8, 9, 12, 15, 24) against a fresh stream per width, so a
// width-specific shift/mask divergence cannot hide behind the random sweep.
func TestParityGetBitsFixedWidths(t *testing.T) {
	requireStrict(t)
	for _, n := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 15, 16, 18, 20, 24} {
		p := newPair(makeRandomBytes(uint64(n)*7+3, 1024))
		for i := 0; i < 256; i++ {
			gotC, gotN := p.getBits(n)
			require.Equal(t, gotC, gotN, "value mismatch n=%d step=%d", n, i)
			require.Equal(t, p.c.pos(), p.bs.Pos, "pos mismatch n=%d step=%d", n, i)
		}
		p.free()
	}
}

// TestParityGetBitsOverrun drives the reader past its limit. minimp3's
// get_bits advances pos by n and returns 0 once pos exceeds limit (without
// consuming any backing bytes); the Go port mirrors this. The test reads to
// the very edge of a short stream and then beyond, asserting both the value
// (0) and the advanced-but-overrun pos stay in lock-step.
func TestParityGetBitsOverrun(t *testing.T) {
	requireStrict(t)
	// 5-byte stream = 40 bits. Read widths chosen to land exactly on the
	// limit and then overshoot it.
	stream := makeRandomBytes(303, 5)
	p := newPair(stream)
	defer p.free()
	for _, n := range []int{8, 8, 8, 8, 8, 1, 7, 12, 24} {
		gotC, gotN := p.getBits(n)
		require.Equal(t, gotC, gotN, "overrun value mismatch (n=%d, pos=%d)", n, p.bs.Pos)
		require.Equal(t, p.c.pos(), p.bs.Pos, "overrun pos mismatch (n=%d)", n)
		require.Equal(t, p.c.limit(), p.bs.Limit, "overrun limit mismatch (n=%d)", n)
	}
	require.Greater(t, p.bs.Pos, p.bs.Limit, "expected the sweep to overrun the limit")
}

// TestParityGetBitsZeroWidth pins the n==0 corner: get_bits returns 0 and
// leaves pos unchanged (pos += 0) in both implementations, interleaved with
// real reads so a wrong zero-width side effect would surface downstream.
func TestParityGetBitsZeroWidth(t *testing.T) {
	requireStrict(t)
	p := newPair(makeRandomBytes(404, 256))
	defer p.free()
	for i := 0; i < 64; i++ {
		gotC, gotN := p.getBits(0)
		require.Equal(t, gotC, gotN, "zero-width value mismatch at step %d", i)
		require.Equal(t, p.c.pos(), p.bs.Pos, "zero-width pos mismatch at step %d", i)
		gotC, gotN = p.getBits(7) // odd width to desync byte alignment
		require.Equal(t, gotC, gotN, "follow-up value mismatch at step %d", i)
		require.Equal(t, p.c.pos(), p.bs.Pos, "follow-up pos mismatch at step %d", i)
	}
}

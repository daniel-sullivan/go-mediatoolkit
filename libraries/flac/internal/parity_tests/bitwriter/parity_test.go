//go:build cgo

package bitwriter

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// writerIface is the common surface driven on both the cgo and native
// bitwriters.
type writerIface interface {
	WriteZeroes(bits uint32) bool
	WriteRawUint32(val, bits uint32) bool
	WriteRawInt32(val int32, bits uint32) bool
	WriteRawUint64(val uint64, bits uint32) bool
	WriteRawInt64(val int64, bits uint32) bool
	WriteRawUint32LittleEndian(val uint32) bool
	WriteByteBlock(vals []byte) bool
	WriteUnaryUnsigned(val uint32) bool
	WriteRiceSignedBlock(vals []int32, parameter uint32) bool
	WriteUTF8Uint32(val uint32) bool
	WriteUTF8Uint64(val uint64) bool
	ZeroPadToByteBoundary() bool
	IsByteAligned() bool
	GetInputBitsUnconsumed() uint32
	GetBuffer() ([]byte, bool)
	GetWriteCRC16() (uint16, bool)
	GetWriteCRC8() (byte, bool)
}

// pair drives a cgo + native writer over the same sequence.
type pair struct {
	c *cgoBitWriter
	n *nativeBitWriter
}

func newPair() *pair {
	return &pair{c: newCgoBitWriter(), n: newNativeBitWriter()}
}

func (p *pair) free() {
	p.c.free()
	p.n.free()
}

// both invokes fn on each writer and asserts the bool results agree.
func (p *pair) both(t *testing.T, msg string, fn func(w writerIface) bool) {
	t.Helper()
	okC := fn(p.c)
	okN := fn(p.n)
	require.Equal(t, okC, okN, "ok mismatch: %s", msg)
}

// assertBufEqual checks that both writers emit byte-identical streams
// and the same input-bit count.
func (p *pair) assertBufEqual(t *testing.T, msg string) {
	t.Helper()
	require.Equal(t, p.c.GetInputBitsUnconsumed(), p.n.GetInputBitsUnconsumed(), "bit count: %s", msg)
	require.Equal(t, p.c.IsByteAligned(), p.n.IsByteAligned(), "alignment: %s", msg)
	// libFLAC's FLAC__bitwriter_get_buffer asserts the writer is
	// byte-aligned (bitwriter.c:249); all real callers zero-pad first.
	// Mirror that here so both sides flush an identical, byte-aligned
	// stream before the byte-for-byte comparison.
	okC := p.c.ZeroPadToByteBoundary()
	okN := p.n.ZeroPadToByteBoundary()
	require.Equal(t, okC, okN, "zero-pad ok: %s", msg)
	bc, okC := p.c.GetBuffer()
	bn, okN := p.n.GetBuffer()
	require.Equal(t, okC, okN, "GetBuffer ok: %s", msg)
	if !okC {
		return
	}
	require.Equal(t, bc, bn, "buffer bytes: %s", msg)
}

// ── single entry-point sweeps ───────────────────────────────────────

func TestParityWriteRawUint32(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	p := newPair()
	defer p.free()
	for i := 0; i < 2000; i++ {
		bits := uint32(r.IntN(33)) // 0..32
		var val uint32
		if bits == 32 {
			val = r.Uint32()
		} else if bits > 0 {
			val = r.Uint32() & ((1 << bits) - 1)
		}
		p.both(t, "WriteRawUint32", func(w writerIface) bool { return w.WriteRawUint32(val, bits) })
	}
	p.assertBufEqual(t, "WriteRawUint32 final")
}

func TestParityWriteRawInt32(t *testing.T) {
	r := rand.New(rand.NewPCG(21, 22))
	p := newPair()
	defer p.free()
	for i := 0; i < 2000; i++ {
		bits := uint32(r.IntN(32) + 1) // 1..32
		val := int32(r.Uint32())
		p.both(t, "WriteRawInt32", func(w writerIface) bool { return w.WriteRawInt32(val, bits) })
	}
	p.assertBufEqual(t, "WriteRawInt32 final")
}

func TestParityWriteRawUint64(t *testing.T) {
	r := rand.New(rand.NewPCG(31, 32))
	p := newPair()
	defer p.free()
	for i := 0; i < 2000; i++ {
		bits := uint32(r.IntN(65)) // 0..64
		var val uint64
		if bits == 64 {
			val = r.Uint64()
		} else if bits > 0 {
			val = r.Uint64() & ((1 << bits) - 1)
		}
		p.both(t, "WriteRawUint64", func(w writerIface) bool { return w.WriteRawUint64(val, bits) })
	}
	p.assertBufEqual(t, "WriteRawUint64 final")
}

func TestParityWriteRawInt64(t *testing.T) {
	r := rand.New(rand.NewPCG(41, 42))
	p := newPair()
	defer p.free()
	for i := 0; i < 2000; i++ {
		bits := uint32(r.IntN(64) + 1) // 1..64
		val := int64(r.Uint64())
		p.both(t, "WriteRawInt64", func(w writerIface) bool { return w.WriteRawInt64(val, bits) })
	}
	p.assertBufEqual(t, "WriteRawInt64 final")
}

func TestParityWriteZeroes(t *testing.T) {
	r := rand.New(rand.NewPCG(51, 52))
	p := newPair()
	defer p.free()
	for i := 0; i < 1000; i++ {
		// Interleave a few raw bits so accum starts off-word-boundary.
		nb := uint32(r.IntN(20)) // 0..19 bits
		var v uint32
		if nb > 0 {
			v = r.Uint32() & ((1 << nb) - 1)
		}
		p.both(t, "raw-pre-zero", func(w writerIface) bool { return w.WriteRawUint32(v, nb) })
		z := uint32(r.IntN(200))
		p.both(t, "WriteZeroes", func(w writerIface) bool { return w.WriteZeroes(z) })
	}
	p.assertBufEqual(t, "WriteZeroes final")
}

func TestParityWriteUnaryUnsigned(t *testing.T) {
	r := rand.New(rand.NewPCG(61, 62))
	p := newPair()
	defer p.free()
	for i := 0; i < 1000; i++ {
		val := uint32(r.IntN(300)) // spans <32 and >=32 paths
		p.both(t, "WriteUnaryUnsigned", func(w writerIface) bool { return w.WriteUnaryUnsigned(val) })
	}
	p.assertBufEqual(t, "WriteUnaryUnsigned final")
}

func TestParityWriteRawUint32LittleEndian(t *testing.T) {
	r := rand.New(rand.NewPCG(71, 72))
	p := newPair()
	defer p.free()
	for i := 0; i < 500; i++ {
		val := r.Uint32()
		p.both(t, "LE32", func(w writerIface) bool { return w.WriteRawUint32LittleEndian(val) })
	}
	p.assertBufEqual(t, "LE32 final")
}

func TestParityWriteByteBlock(t *testing.T) {
	r := rand.New(rand.NewPCG(81, 82))
	p := newPair()
	defer p.free()
	for i := 0; i < 200; i++ {
		n := r.IntN(40)
		vals := make([]byte, n)
		for j := range vals {
			vals[j] = byte(r.Uint32())
		}
		p.both(t, "WriteByteBlock", func(w writerIface) bool { return w.WriteByteBlock(vals) })
	}
	p.assertBufEqual(t, "WriteByteBlock final")
}

func TestParityWriteUTF8Uint32(t *testing.T) {
	r := rand.New(rand.NewPCG(91, 92))
	p := newPair()
	defer p.free()
	// Cover every length class plus the 31-bit boundary rejection.
	fixed := []uint32{0, 0x7F, 0x80, 0x7FF, 0x800, 0xFFFF, 0x10000,
		0x1FFFFF, 0x200000, 0x3FFFFFF, 0x4000000, 0x7FFFFFFF, 0x80000000}
	for _, v := range fixed {
		val := v
		p.both(t, "UTF8u32 fixed", func(w writerIface) bool { return w.WriteUTF8Uint32(val) })
	}
	for i := 0; i < 500; i++ {
		val := r.Uint32() & 0x7FFFFFFF
		p.both(t, "UTF8u32 rand", func(w writerIface) bool { return w.WriteUTF8Uint32(val) })
	}
	p.assertBufEqual(t, "UTF8u32 final")
}

func TestParityWriteUTF8Uint64(t *testing.T) {
	r := rand.New(rand.NewPCG(101, 102))
	p := newPair()
	defer p.free()
	fixed := []uint64{0, 0x7F, 0x80, 0x7FF, 0x800, 0xFFFF, 0x10000,
		0x1FFFFF, 0x200000, 0x3FFFFFF, 0x4000000, 0x7FFFFFFF, 0x80000000,
		0xFFFFFFFFF, 0x1000000000}
	for _, v := range fixed {
		val := v
		p.both(t, "UTF8u64 fixed", func(w writerIface) bool { return w.WriteUTF8Uint64(val) })
	}
	for i := 0; i < 500; i++ {
		val := r.Uint64() & 0xFFFFFFFFF // 36 bits
		p.both(t, "UTF8u64 rand", func(w writerIface) bool { return w.WriteUTF8Uint64(val) })
	}
	p.assertBufEqual(t, "UTF8u64 final")
}

func TestParityWriteRiceSignedBlock(t *testing.T) {
	r := rand.New(rand.NewPCG(111, 112))
	for iter := 0; iter < 200; iter++ {
		p := newPair()
		parameter := uint32(r.IntN(30)) // 0..29; libFLAC asserts < 31
		nvals := r.IntN(200) + 1
		vals := make([]int32, nvals)
		for j := range vals {
			switch r.IntN(8) {
			case 0:
				// large magnitude => many msbits (split path)
				vals[j] = int32(r.Uint32())
			default:
				vals[j] = int32(r.IntN(1<<16)) - (1 << 15)
			}
		}
		// Optionally seed a partial-word offset first.
		if r.IntN(2) == 0 {
			nb := uint32(r.IntN(40))
			var v uint64
			if nb > 0 {
				v = r.Uint64() & ((1 << nb) - 1)
			}
			p.both(t, "rice-pre", func(w writerIface) bool { return w.WriteRawUint64(v, nb) })
		}
		p.both(t, "WriteRiceSignedBlock", func(w writerIface) bool {
			return w.WriteRiceSignedBlock(vals, parameter)
		})
		p.assertBufEqual(t, "WriteRiceSignedBlock")
		p.free()
	}
}

// ── interleaved sweep ───────────────────────────────────────────────

func TestParityInterleaved(t *testing.T) {
	r := rand.New(rand.NewPCG(201, 202))
	p := newPair()
	defer p.free()
	for i := 0; i < 5000; i++ {
		switch r.IntN(8) {
		case 0:
			bits := uint32(r.IntN(33))
			var v uint32
			if bits == 32 {
				v = r.Uint32()
			} else if bits > 0 {
				v = r.Uint32() & ((1 << bits) - 1)
			}
			p.both(t, "raw32", func(w writerIface) bool { return w.WriteRawUint32(v, bits) })
		case 1:
			z := uint32(r.IntN(100))
			p.both(t, "zeroes", func(w writerIface) bool { return w.WriteZeroes(z) })
		case 2:
			u := uint32(r.IntN(150))
			p.both(t, "unary", func(w writerIface) bool { return w.WriteUnaryUnsigned(u) })
		case 3:
			bits := uint32(r.IntN(65))
			var v uint64
			if bits == 64 {
				v = r.Uint64()
			} else if bits > 0 {
				v = r.Uint64() & ((1 << bits) - 1)
			}
			p.both(t, "raw64", func(w writerIface) bool { return w.WriteRawUint64(v, bits) })
		case 4:
			// Draw the value ONCE outside the closure; p.both invokes the
			// closure twice (C then native), so an inline r.Uint32() would
			// feed the two writers different inputs.
			uv := r.Uint32() & 0x7FFFFFFF
			p.both(t, "utf8u32", func(w writerIface) bool { return w.WriteUTF8Uint32(uv) })
		case 5:
			param := uint32(r.IntN(30))
			nv := r.IntN(20) + 1
			vals := make([]int32, nv)
			for j := range vals {
				vals[j] = int32(r.IntN(1<<14)) - (1 << 13)
			}
			p.both(t, "rice", func(w writerIface) bool { return w.WriteRiceSignedBlock(vals, param) })
		case 6:
			n := r.IntN(10)
			vals := make([]byte, n)
			for j := range vals {
				vals[j] = byte(r.Uint32())
			}
			p.both(t, "byteblock", func(w writerIface) bool { return w.WriteByteBlock(vals) })
		case 7:
			p.both(t, "zeropad", func(w writerIface) bool { return w.ZeroPadToByteBoundary() })
		}
	}
	p.assertBufEqual(t, "interleaved final")
}

// ── CRC + round-trip ────────────────────────────────────────────────

func TestParityWriteCRC(t *testing.T) {
	r := rand.New(rand.NewPCG(301, 302))
	for cycle := 0; cycle < 100; cycle++ {
		p := newPair()
		for i := 0; i < 1+r.IntN(40); i++ {
			bits := uint32(r.IntN(33))
			var v uint32
			if bits == 32 {
				v = r.Uint32()
			} else if bits > 0 {
				v = r.Uint32() & ((1 << bits) - 1)
			}
			p.both(t, "crc-fill", func(w writerIface) bool { return w.WriteRawUint32(v, bits) })
		}
		p.both(t, "crc-pad", func(w writerIface) bool { return w.ZeroPadToByteBoundary() })

		crc16C, okC := p.c.GetWriteCRC16()
		crc16N, okN := p.n.GetWriteCRC16()
		require.Equal(t, okC, okN, "crc16 ok cycle %d", cycle)
		require.Equal(t, crc16C, crc16N, "crc16 cycle %d", cycle)

		crc8C, okC := p.c.GetWriteCRC8()
		crc8N, okN := p.n.GetWriteCRC8()
		require.Equal(t, okC, okN, "crc8 ok cycle %d", cycle)
		require.Equal(t, crc8C, crc8N, "crc8 cycle %d", cycle)

		p.free()
	}
}

// TestRoundTripThroughBitReader feeds bytes produced by the Go
// bitwriter back through the already-ported Go BitReader and checks the
// values survive a write/read round trip.
func TestRoundTripThroughBitReader(t *testing.T) {
	r := rand.New(rand.NewPCG(401, 402))
	type op struct {
		bits uint32
		val  uint32
	}
	for cycle := 0; cycle < 200; cycle++ {
		n := newNativeBitWriter()
		var ops []op
		for i := 0; i < r.IntN(60)+1; i++ {
			bits := uint32(r.IntN(32) + 1) // 1..32
			var v uint32
			if bits == 32 {
				v = r.Uint32()
			} else {
				v = r.Uint32() & ((1 << bits) - 1)
			}
			require.True(t, n.WriteRawUint32(v, bits), "write cycle %d op %d", cycle, i)
			ops = append(ops, op{bits, v})
		}
		require.True(t, n.ZeroPadToByteBoundary())
		buf, ok := n.GetBuffer()
		require.True(t, ok)
		n.free()

		// Read back.
		off := 0
		br := nativeflac.NewBitReader()
		br.Init(func(dst []byte) (uint, bool) {
			avail := len(buf) - off
			if avail <= 0 {
				return 0, false
			}
			w := len(dst)
			if w > avail {
				w = avail
			}
			copy(dst, buf[off:off+w])
			off += w
			return uint(w), true
		})
		for i, o := range ops {
			got, ok := br.ReadRawUint32(o.bits)
			require.True(t, ok, "read cycle %d op %d", cycle, i)
			require.Equal(t, o.val, got, "round-trip cycle %d op %d (bits=%d)", cycle, i, o.bits)
		}
		br.Free()
	}
}

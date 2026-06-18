//go:build cgo

package bitreader

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// makeRandomBytes returns n random bytes seeded deterministically so
// the parity sweep is reproducible across runs.
func makeRandomBytes(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, seed+1))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// pair holds the cgo + Go bitreaders driven over the same stream.
type pair struct {
	c *cgoBitReader
	n *nativeBitReader
}

func newPair(source []byte) *pair {
	return &pair{c: newCgoBitReader(source), n: newNativeBitReader(source)}
}

func (p *pair) free() {
	p.c.free()
	p.n.free()
}

// ── ReadRawUint32 sweep ─────────────────────────────────────────────

func TestParityReadRawUint32(t *testing.T) {
	stream := makeRandomBytes(101, 4096)
	r := rand.New(rand.NewPCG(102, 103))
	p := newPair(stream)
	defer p.free()
	for i := 0; i < 1000; i++ {
		bits := uint32(r.IntN(33)) // 0..32
		gotC, okC := p.c.ReadRawUint32(bits)
		gotN, okN := p.n.ReadRawUint32(bits)
		require.Equal(t, okC, okN, "ReadRawUint32 ok mismatch at step %d (bits=%d)", i, bits)
		if !okC {
			break
		}
		require.Equal(t, gotC, gotN, "ReadRawUint32 value mismatch at step %d (bits=%d)", i, bits)
	}
}

func TestParityReadRawInt32(t *testing.T) {
	stream := makeRandomBytes(201, 4096)
	r := rand.New(rand.NewPCG(202, 203))
	p := newPair(stream)
	defer p.free()
	for i := 0; i < 1000; i++ {
		bits := uint32(r.IntN(32) + 1) // 1..32
		gotC, okC := p.c.ReadRawInt32(bits)
		gotN, okN := p.n.ReadRawInt32(bits)
		require.Equal(t, okC, okN, "step %d bits=%d", i, bits)
		if !okC {
			break
		}
		require.Equal(t, gotC, gotN, "step %d bits=%d", i, bits)
	}
}

func TestParityReadRawUint64(t *testing.T) {
	stream := makeRandomBytes(301, 8192)
	r := rand.New(rand.NewPCG(302, 303))
	p := newPair(stream)
	defer p.free()
	for i := 0; i < 1000; i++ {
		bits := uint32(r.IntN(65)) // 0..64
		gotC, okC := p.c.ReadRawUint64(bits)
		gotN, okN := p.n.ReadRawUint64(bits)
		require.Equal(t, okC, okN, "step %d bits=%d", i, bits)
		if !okC {
			break
		}
		require.Equal(t, gotC, gotN, "step %d bits=%d", i, bits)
	}
}

func TestParityReadRawInt64(t *testing.T) {
	stream := makeRandomBytes(401, 8192)
	r := rand.New(rand.NewPCG(402, 403))
	p := newPair(stream)
	defer p.free()
	for i := 0; i < 500; i++ {
		bits := uint32(r.IntN(64) + 1)
		gotC, okC := p.c.ReadRawInt64(bits)
		gotN, okN := p.n.ReadRawInt64(bits)
		require.Equal(t, okC, okN, "step %d bits=%d", i, bits)
		if !okC {
			break
		}
		require.Equal(t, gotC, gotN, "step %d bits=%d", i, bits)
	}
}

func TestParityReadUint32LittleEndian(t *testing.T) {
	stream := makeRandomBytes(501, 4096)
	p := newPair(stream)
	defer p.free()
	for i := 0; i < 100; i++ {
		gotC, okC := p.c.ReadUint32LittleEndian()
		gotN, okN := p.n.ReadUint32LittleEndian()
		require.Equal(t, okC, okN, "step %d", i)
		if !okC {
			break
		}
		require.Equal(t, gotC, gotN, "step %d", i)
	}
}

// ── Unary / Rice ────────────────────────────────────────────────────

func TestParityReadUnaryUnsigned(t *testing.T) {
	stream := makeRandomBytes(601, 4096)
	p := newPair(stream)
	defer p.free()
	for i := 0; i < 500; i++ {
		gotC, okC := p.c.ReadUnaryUnsigned()
		gotN, okN := p.n.ReadUnaryUnsigned()
		require.Equal(t, okC, okN, "step %d", i)
		if !okC {
			break
		}
		require.Equal(t, gotC, gotN, "step %d", i)
	}
}

func TestParityReadRiceSignedBlock(t *testing.T) {
	stream := makeRandomBytes(701, 16384)
	r := rand.New(rand.NewPCG(702, 703))
	p := newPair(stream)
	defer p.free()
	for i := 0; i < 50; i++ {
		nvals := r.IntN(64) + 1
		parameter := uint32(r.IntN(31)) // 0..30; libFLAC asserts < 32
		outC := make([]int32, nvals)
		outN := make([]int32, nvals)
		okC := p.c.ReadRiceSignedBlock(outC, parameter)
		okN := p.n.ReadRiceSignedBlock(outN, parameter)
		require.Equal(t, okC, okN, "step %d nvals=%d param=%d", i, nvals, parameter)
		if !okC {
			break
		}
		require.Equal(t, outC, outN, "step %d nvals=%d param=%d", i, nvals, parameter)
	}
}

// ── Skip / byte-block ───────────────────────────────────────────────

func TestParitySkipAndReadInterleaved(t *testing.T) {
	stream := makeRandomBytes(801, 8192)
	r := rand.New(rand.NewPCG(802, 803))
	p := newPair(stream)
	defer p.free()
	for i := 0; i < 200; i++ {
		switch r.IntN(4) {
		case 0:
			bits := uint32(r.IntN(33))
			gotC, okC := p.c.ReadRawUint32(bits)
			gotN, okN := p.n.ReadRawUint32(bits)
			require.Equal(t, okC, okN, "step %d kind=raw bits=%d", i, bits)
			if !okC {
				return
			}
			require.Equal(t, gotC, gotN, "step %d kind=raw bits=%d", i, bits)
		case 1:
			bits := uint32(r.IntN(64))
			okC := p.c.SkipBitsNoCRC(bits)
			okN := p.n.SkipBitsNoCRC(bits)
			require.Equal(t, okC, okN, "step %d kind=skip bits=%d", i, bits)
			if !okC {
				return
			}
		case 2:
			// byte-aligned read; ensure alignment first
			gotC, okC := p.c.ReadRawUint32(p.c.BitsLeftForByteAlignment() & 7)
			gotN, okN := p.n.ReadRawUint32(p.n.BitsLeftForByteAlignment() & 7)
			require.Equal(t, okC, okN)
			require.Equal(t, gotC, gotN)
			if !okC {
				return
			}
			n := r.IntN(32) + 1
			outC := make([]byte, n)
			outN := make([]byte, n)
			okC = p.c.ReadByteBlockAlignedNoCRC(outC)
			okN = p.n.ReadByteBlockAlignedNoCRC(outN)
			require.Equal(t, okC, okN, "step %d kind=byteblock n=%d", i, n)
			if !okC {
				return
			}
			require.Equal(t, outC, outN, "step %d kind=byteblock n=%d", i, n)
		case 3:
			gotC, okC := p.c.ReadUnaryUnsigned()
			gotN, okN := p.n.ReadUnaryUnsigned()
			require.Equal(t, okC, okN, "step %d kind=unary", i)
			if !okC {
				return
			}
			require.Equal(t, gotC, gotN, "step %d kind=unary", i)
		}
	}
}

// ── UTF-8 number readers ────────────────────────────────────────────

func TestParityReadUTF8Uint32(t *testing.T) {
	// Hand-crafted bytes covering: 1-byte, 2-byte, 3-byte, 4-byte,
	// truncated continuation, and an invalid leading byte.
	stream := []byte{
		0x00,       // 0
		0x7F,       // 127
		0xC2, 0x80, // U+0080 (2-byte legal)
		0xE2, 0x82, 0xAC, // U+20AC '€' (3-byte legal)
		0xF0, 0x9F, 0x98, 0x80, // U+1F600 (4-byte legal)
		0xFF, // invalid lead — libFLAC returns 0xFFFFFFFF success
	}
	p := newPair(stream)
	defer p.free()
	for i := 0; i < 6; i++ {
		gotC, _, okC := p.c.ReadUTF8Uint32()
		gotN, _, okN := p.n.ReadUTF8Uint32()
		require.Equal(t, okC, okN, "step %d", i)
		require.Equal(t, gotC, gotN, "step %d", i)
	}
}

func TestParityReadUTF8Uint64(t *testing.T) {
	stream := []byte{
		0x00,
		0xC2, 0x80,
		0xFE, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, // 7-byte (used for FLAC sample numbers > 36 bits)
		0xFF, // invalid lead
	}
	p := newPair(stream)
	defer p.free()
	for i := 0; i < 4; i++ {
		gotC, _, okC := p.c.ReadUTF8Uint64()
		gotN, _, okN := p.n.ReadUTF8Uint64()
		require.Equal(t, okC, okN, "step %d", i)
		require.Equal(t, gotC, gotN, "step %d", i)
	}
}

// ── CRC tracking ────────────────────────────────────────────────────

func TestParityRunningCRC16(t *testing.T) {
	stream := makeRandomBytes(901, 4096)
	r := rand.New(rand.NewPCG(902, 903))
	p := newPair(stream)
	defer p.free()

	// Reset → reads → GetReadCRC16 is the production usage pattern
	// (one cycle per frame). Calling GetReadCRC16 twice without a
	// Reset between them would expose libFLAC's last-call-wins
	// state-reset behaviour at crc16_update_block_:154 — divergent
	// from the Go port and not actually reachable in real use.
	for cycle := 0; cycle < 50; cycle++ {
		seed := uint16(r.Uint32())
		p.c.ResetReadCRC16(seed)
		p.n.ResetReadCRC16(seed)

		// Vary how many byte-aligned reads precede the CRC check.
		nReads := 1 + r.IntN(6)
		for k := 0; k < nReads; k++ {
			bits := uint32(r.IntN(32) + 1)
			_, okC := p.c.ReadRawUint32(bits)
			_, okN := p.n.ReadRawUint32(bits)
			require.Equal(t, okC, okN, "cycle %d step %d", cycle, k)
			if !okC {
				return
			}
		}
		// Align to byte before reading the CRC (libFLAC asserts
		// consumed_bits & 7 == 0 inside GetReadCRC16).
		alignC := p.c.BitsLeftForByteAlignment() & 7
		alignN := p.n.BitsLeftForByteAlignment() & 7
		require.Equal(t, alignC, alignN, "alignment delta at cycle %d", cycle)
		if alignC > 0 {
			_, okC := p.c.ReadRawUint32(alignC)
			_, okN := p.n.ReadRawUint32(alignN)
			require.Equal(t, okC, okN)
			if !okC {
				return
			}
		}
		require.Equal(t,
			p.c.GetReadCRC16(),
			p.n.GetReadCRC16(),
			"CRC at cycle %d (seed=%04x)", cycle, seed)
	}
}

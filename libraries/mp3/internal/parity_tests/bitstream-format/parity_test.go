//go:build cgo

package bitstreamformat

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// These parity tests pin the pure-Go nativemp3 bitstream-format port against
// the vendored C minimp3 reference (oracle.c). Every routine is driven on both
// sides over identical fabricated input and the outcomes must be bit-for-bit
// equal.
//
// The slice is integer-only — its results are independent of FMA/vectorization
// — but the bit-exact assertions are still gated behind nativemp3.StrictMode
// per the FP-parity convention, so a bare `go test` is clean and the strict
// run (mp3_strict + the FP CGO env) is the authoritative bit-exact gate.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("bit-exact parity asserted only under -tags=mp3_strict (FP env via mise //libraries/mp3:parity)")
	}
}

// candidateHeaders enumerates a broad set of 4-byte MPEG audio frame headers
// across MPEG-1/2/2.5, Layer I/II/III, every bitrate and sample-rate index,
// padding/CRC/mono toggles, plus deliberately invalid forms (reserved layer,
// bitrate index 15, sample-rate index 3, bad sync) to exercise the validity
// and field-decode paths the same way on both sides.
func candidateHeaders() [][]byte {
	var out [][]byte
	// h[1] high nibble / version+layer combos: 0xFx (MPEG1/2 + each layer)
	// and 0xEx (MPEG2.5). Bit0 of h[1] toggles CRC.
	b1set := []byte{
		0xFF, 0xFE, 0xFD, 0xFC, 0xFB, 0xFA, 0xF9, 0xF8, // MPEG1 (0x18) layers
		0xF7, 0xF6, 0xF5, 0xF4, 0xF3, 0xF2, 0xF1, 0xF0, // MPEG2 (0x10) layers
		0xE3, 0xE2, 0xE5, 0xE4, // MPEG2.5 (0x00) — only 0xE2/0xE3 pass sync gate
		0xC0, // bad sync in h[1]
	}
	for _, b1 := range b1set {
		for bitrate := 0; bitrate < 16; bitrate++ {
			for srate := 0; srate < 4; srate++ {
				for _, pad := range []byte{0x00, 0x02} {
					h2 := byte(bitrate<<4) | byte(srate<<2) | pad
					for _, h3 := range []byte{0x00, 0x04, 0xC0, 0x60} {
						out = append(out, []byte{0xFF, b1, h2, h3})
					}
				}
			}
		}
	}
	// A handful with a non-0xFF first byte to drive the sync check.
	out = append(out, []byte{0x00, 0xFB, 0x90, 0x04})
	out = append(out, []byte{0xFE, 0xFB, 0x90, 0x04})
	return out
}

// TestParityHeaderAccessors compares every HDR_* field accessor / predicate
// for the full candidate header set. hdr_bitrate_kbps / hdr_sample_rate_hz /
// hdr_frame_bytes are only meaningful on structurally valid headers (the C
// indexes tables by layer-1 and bitrate which assume validity), so those are
// asserted on valid headers only — matching how minimp3 only ever calls them
// after hdr_valid succeeds.
func TestParityHeaderAccessors(t *testing.T) {
	requireStrict(t)
	for _, h := range candidateHeaders() {
		require.Equal(t, cgoHdrIsMono(h), nativeHdrIsMono(h), "is_mono %x", h)
		require.Equal(t, cgoHdrIsFreeFormat(h), nativeHdrIsFreeFormat(h), "is_free_format %x", h)
		require.Equal(t, cgoHdrIsCRC(h), nativeHdrIsCRC(h), "is_crc %x", h)
		require.Equal(t, cgoHdrTestPadding(h), nativeHdrTestPadding(h), "test_padding %x", h)
		require.Equal(t, cgoHdrTestMPEG1(h), nativeHdrTestMPEG1(h), "test_mpeg1 %x", h)
		require.Equal(t, cgoHdrTestNotMPEG25(h), nativeHdrTestNotMPEG25(h), "test_not_mpeg25 %x", h)
		require.Equal(t, cgoHdrGetLayer(h), nativeHdrGetLayer(h), "get_layer %x", h)
		require.Equal(t, cgoHdrGetBitrate(h), nativeHdrGetBitrate(h), "get_bitrate %x", h)
		require.Equal(t, cgoHdrGetSampleRate(h), nativeHdrGetSampleRate(h), "get_sample_rate %x", h)
		require.Equal(t, cgoHdrGetMySampleRate(h), nativeHdrGetMySampleRate(h), "get_my_sample_rate %x", h)
		require.Equal(t, cgoHdrIsFrame576(h), nativeHdrIsFrame576(h), "is_frame_576 %x", h)
		require.Equal(t, cgoHdrIsLayer1(h), nativeHdrIsLayer1(h), "is_layer_1 %x", h)
		require.Equal(t, cgoHdrPadding(h), nativeHdrPadding(h), "padding %x", h)
		require.Equal(t, cgoHdrFrameSamples(h), nativeHdrFrameSamples(h), "frame_samples %x", h)

		cValid := cgoHdrValid(h)
		require.Equal(t, cValid, nativeHdrValid(h), "valid %x", h)
		if cValid {
			require.Equal(t, cgoHdrBitrateKbps(h), nativeHdrBitrateKbps(h), "bitrate_kbps %x", h)
			require.Equal(t, cgoHdrSampleRateHz(h), nativeHdrSampleRateHz(h), "sample_rate_hz %x", h)
			require.Equal(t, cgoHdrFrameBytes(h, 0), nativeHdrFrameBytes(h, 0), "frame_bytes %x", h)
			require.Equal(t, cgoHdrFrameBytes(h, 1234), nativeHdrFrameBytes(h, 1234), "frame_bytes ff %x", h)
		}
	}
}

// TestParityHdrCompare cross-compares pairs of valid headers through
// hdr_compare (version/layer/sample-rate/free-format equality).
func TestParityHdrCompare(t *testing.T) {
	requireStrict(t)
	var valid [][]byte
	for _, h := range candidateHeaders() {
		if cgoHdrValid(h) {
			valid = append(valid, h)
		}
	}
	r := rand.New(rand.NewPCG(11, 13))
	for i := 0; i < 4000; i++ {
		h1 := valid[r.IntN(len(valid))]
		h2 := valid[r.IntN(len(valid))]
		require.Equal(t, cgoHdrCompare(h1, h2), nativeHdrCompare(h1, h2),
			"hdr_compare %x vs %x", h1, h2)
	}
}

// TestParityGetBits sweeps the MSB-first bit reader over random byte streams,
// drawing random field widths (including reads that run past the limit, where
// minimp3 returns 0 but still advances pos).
func TestParityGetBits(t *testing.T) {
	requireStrict(t)
	for seed := uint64(1); seed <= 8; seed++ {
		stream := makeRandomBytes(seed*7+1, 256)
		c := newCgoBitStream(stream)
		n := newNativeBitStream(stream)
		r := rand.New(rand.NewPCG(seed, seed+99))
		defer c.free()
		for step := 0; step < 600; step++ {
			bits := r.IntN(25) // 0..24, the widths minimp3 uses
			gc := c.getBits(bits)
			gn := n.getBits(bits)
			require.Equal(t, gc, gn, "get_bits seed=%d step=%d bits=%d", seed, step, bits)
			require.Equal(t, c.pos(), n.pos(), "pos seed=%d step=%d", seed, step)
			require.Equal(t, c.limit(), n.limit(), "limit seed=%d step=%d", seed, step)
		}
	}
}

// makeFrame builds a frameLen-byte block whose first four bytes are h.
func makeFrame(h []byte, frameLen int) []byte {
	f := make([]byte, frameLen)
	copy(f, h)
	return f
}

// TestParityMatchFrame drives mp3d_match_frame over self-consistent runs of
// identical frames and over runs with a corrupted later header.
func TestParityMatchFrame(t *testing.T) {
	requireStrict(t)
	h := []byte{0xFF, 0xFB, 0x90, 0x04} // MPEG-1 L3 128 kbps 44.1k → 417 bytes
	const frameLen = 417
	// Clean run.
	var buf []byte
	for i := 0; i < nativemp3.MaxFrameSyncMatches+3; i++ {
		buf = append(buf, makeFrame(h, frameLen)...)
	}
	require.Equal(t, cgoMatchFrame(buf, len(buf), frameLen),
		nativeMatchFrame(buf, len(buf), frameLen), "clean run")

	// Run with the third frame's header corrupted to a different format.
	corrupt := append([]byte(nil), buf...)
	corrupt[2*frameLen+1] = 0xFD // different layer → hdr_compare fails
	require.Equal(t, cgoMatchFrame(corrupt, len(corrupt), frameLen),
		nativeMatchFrame(corrupt, len(corrupt), frameLen), "corrupt run")

	// Truncated run (only a couple of frames).
	short := buf[:2*frameLen+2]
	require.Equal(t, cgoMatchFrame(short, len(short), frameLen),
		nativeMatchFrame(short, len(short), frameLen), "short run")
}

// TestParityFindFrame exercises mp3d_find_frame across leading garbage, clean
// runs at several formats, and short/empty inputs. The returned (offset,
// frame_bytes) and the mutated free_format_bytes must agree.
func TestParityFindFrame(t *testing.T) {
	requireStrict(t)
	type fmtCase struct {
		h     []byte
		flen  int
		label string
	}
	cases := []fmtCase{
		{[]byte{0xFF, 0xFB, 0x90, 0x04}, 417, "mpeg1 l3 128k 44.1k"},
		{[]byte{0xFF, 0xFB, 0xA0, 0x04}, 470, "mpeg1 l3 160k 44.1k"},
		{[]byte{0xFF, 0xF3, 0x50, 0x04}, 0, "mpeg2 l3 (computed)"},
	}
	for _, fc := range cases {
		flen := fc.flen
		if flen == 0 {
			// Derive the frame length the same way both sides do, off the
			// oracle, so the fabricated run is self-consistent.
			flen = cgoHdrFrameBytes(fc.h, 0)
		}
		require.Positive(t, flen, "%s flen", fc.label)
		for _, lead := range []int{0, 1, 5} {
			buf := make([]byte, lead)
			for i := 0; i < nativemp3.MaxFrameSyncMatches+3; i++ {
				buf = append(buf, makeFrame(fc.h, flen)...)
			}
			cff, nff := 0, 0
			cOff, cFb := cgoFindFrame(buf, len(buf), &cff)
			nOff, nFb := nativeFindFrame(buf, len(buf), &nff)
			require.Equal(t, cOff, nOff, "%s lead=%d offset", fc.label, lead)
			require.Equal(t, cFb, nFb, "%s lead=%d frame_bytes", fc.label, lead)
			require.Equal(t, cff, nff, "%s lead=%d free_format_bytes", fc.label, lead)
		}
	}

	// Degenerate inputs: empty, sub-header, all-zero.
	for _, buf := range [][]byte{{}, {0xFF}, {0xFF, 0xFB, 0x90}, make([]byte, 64)} {
		cff, nff := 0, 0
		cOff, cFb := cgoFindFrame(buf, len(buf), &cff)
		nOff, nFb := nativeFindFrame(buf, len(buf), &nff)
		require.Equal(t, cOff, nOff, "degenerate len=%d offset", len(buf))
		require.Equal(t, cFb, nFb, "degenerate len=%d frame_bytes", len(buf))
		require.Equal(t, cff, nff, "degenerate len=%d ff", len(buf))
	}
}

// TestParityReservoirRoundTrip exercises L3_save_reservoir +
// L3_restore_reservoir over random prior-frame state and main_data_begin
// back-pointers, including the overflow clamp (> MAX_BITRESERVOIR_BYTES) and
// the under-run case (reserv < main_data_begin → restore returns false).
func TestParityReservoirRoundTrip(t *testing.T) {
	requireStrict(t)
	r := rand.New(rand.NewPCG(404, 405))
	for i := 0; i < 400; i++ {
		// Random prior main data and a consumed bit position.
		mdLen := r.IntN(700) + 1
		md := makeRandomBytes(uint64(i)*3+1, mdLen)
		limitBits := mdLen * 8
		posBits := r.IntN(limitBits + 1)

		cr := newCgoReservoir()
		nr := newNativeReservoir()
		cr.setMaindata(md)
		nr.setMaindata(md)
		cr.setBs(posBits, limitBits)
		nr.setBs(posBits, limitBits)

		cr.saveReservoir()
		nr.saveReservoir()
		require.Equal(t, cr.reserv(), nr.reserv(), "save reserv i=%d", i)
		rc := cr.reserv()
		if rc > 0 {
			require.Equal(t, cr.reservBuf(rc), nr.reservBuf(rc), "save reserv_buf i=%d", i)
		}

		// Now restore in front of a fresh payload.
		payLen := r.IntN(500) + 1
		pay := makeRandomBytes(uint64(i)*5+7, payLen)
		mdb := r.IntN(600) // may exceed reserv to hit the false path
		okC := cr.restoreReservoir(pay, mdb)
		okN := nr.restoreReservoir(pay, mdb)
		require.Equal(t, okC, okN, "restore ok i=%d mdb=%d", i, mdb)

		// The reassembled maindata + the scratch reader state must match.
		bytesHave := rc
		if mdb < bytesHave {
			bytesHave = mdb
		}
		total := bytesHave + payLen
		require.Equal(t, cr.maindata(total), nr.maindata(total), "restore maindata i=%d", i)
		require.Equal(t, cr.bsPos(), nr.bsPos(), "restore bs.pos i=%d", i)
		require.Equal(t, cr.bsLimit(), nr.bsLimit(), "restore bs.limit i=%d", i)

		cr.free()
	}
}

// NOTE: a side-info (L3_read_side_info) parity test is deliberately absent.
// The committed vendored minimp3.h returns the raw main_data_begin back-pointer
// on success, while the Go port (nativemp3.L3ReadSideInfo) returns bs.Pos/8 —
// the two were tracked from different minimp3 revisions, so a parity oracle
// would compare different quantities. The slice is deferred pending a
// reconciliation decision (reported back to the task author), and oracle.c
// does not re-export L3_read_side_info.

// makeRandomBytes returns n deterministically-seeded random bytes.
func makeRandomBytes(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, seed+1))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

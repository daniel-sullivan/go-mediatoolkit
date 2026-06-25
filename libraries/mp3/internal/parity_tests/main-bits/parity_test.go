//go:build cgo

package mainbits

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// requireStrict skips a parity test unless the mp3_strict build tag is
// set. The main-bits slice is integer-only and therefore matches in both
// build modes, but the suite gates uniformly with the FP-bearing slices so
// a bare `go test` stays clean and the canonical
// `mise run //libraries/mp3:parity` (which sets -tags=mp3_strict + the
// scalar CGO flags) is the single bit-exact gate. See the FP-parity
// convention in the add-audio-format skill.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("main-bits parity asserts bit-exactness; run under -tags=mp3_strict (mise run //libraries/mp3:parity)")
	}
}

// headerCorpus is a set of structurally valid MPEG audio frame headers
// spanning MPEG-1/2/2.5, all three layers, mono/stereo, padding, and CRC
// variants. Every header is built so hdr_valid is true on both sides; the
// accessors are then compared field-for-field.
var headerCorpus = [][]byte{
	{0xFF, 0xFB, 0x90, 0x04}, // MPEG-1 L3 128k 44100 stereo no-pad no-crc
	{0xFF, 0xFB, 0x92, 0x04}, // ... with padding
	{0xFF, 0xFA, 0x90, 0x04}, // ... CRC protected
	{0xFF, 0xFB, 0x90, 0xC4}, // ... mono
	{0xFF, 0xFB, 0xA0, 0x04}, // MPEG-1 L3 different bitrate idx
	{0xFF, 0xFD, 0x90, 0x04}, // MPEG-1 Layer II
	{0xFF, 0xFF, 0x90, 0x04}, // MPEG-1 Layer I
	{0xFF, 0xF3, 0x90, 0x04}, // MPEG-2 L3 (frame-576)
	{0xFF, 0xF3, 0x40, 0x04}, // MPEG-2 L3 lower bitrate idx
	{0xFF, 0xE3, 0x90, 0x04}, // MPEG-2.5 L3
	{0xFF, 0xFB, 0x10, 0x04}, // bitrate idx 1
	{0xFF, 0xFB, 0xE0, 0x04}, // bitrate idx 14
	{0xFF, 0xFB, 0x94, 0x04}, // sample-rate idx 1 (48000)
	{0xFF, 0xFB, 0x98, 0x04}, // sample-rate idx 2 (32000)
}

func TestHeaderAccessorsParity(t *testing.T) {
	requireStrict(t)
	for _, h := range headerCorpus {
		require.True(t, nativemp3.HdrValid(h), "go hdr_valid on % x", h)
		require.True(t, cgoHdrValid(h), "c hdr_valid on % x", h)

		assert.Equalf(t, cgoHdrBitrateKbps(h), nativemp3.HdrBitrateKbps(h), "bitrate_kbps % x", h)
		assert.Equalf(t, cgoHdrSampleRateHz(h), nativemp3.HdrSampleRateHz(h), "sample_rate_hz % x", h)
		assert.Equalf(t, cgoHdrFrameSamples(h), nativemp3.HdrFrameSamples(h), "frame_samples % x", h)
		assert.Equalf(t, cgoHdrPadding(h), nativemp3.HdrPadding(h), "padding % x", h)
		for _, ff := range []int{0, 100, 1044, 2304} {
			assert.Equalf(t, cgoHdrFrameBytes(h, ff), nativemp3.HdrFrameBytes(h, ff), "frame_bytes ff=%d % x", ff, h)
		}
	}
}

func TestHdrCompareParity(t *testing.T) {
	requireStrict(t)
	for _, a := range headerCorpus {
		for _, b := range headerCorpus {
			assert.Equalf(t, cgoHdrCompare(a, b), nativemp3.HdrCompare(a, b), "hdr_compare % x vs % x", a, b)
		}
	}
}

func TestGetBitsParity(t *testing.T) {
	requireStrict(t)
	// A handful of byte slabs and read schedules. Each schedule is a
	// sequence of bit-widths; we read the same widths on both sides and
	// compare every returned value plus the final (pos, limit).
	slabs := [][]byte{
		{0xAC, 0x35},
		{0xFF, 0xFB, 0x90, 0x04, 0x12, 0x34, 0x56, 0x78},
		{0x00, 0x01, 0x80, 0x7F, 0xAA, 0x55, 0xCC, 0x33, 0xF0, 0x0F},
	}
	schedules := [][]int{
		{3, 5, 8, 1},
		{1, 1, 1, 1, 1, 1, 1, 1, 9, 17},
		{12, 12, 12, 12, 4, 20},
		{32, 32}, // walk past the limit
	}
	for _, slab := range slabs {
		for _, sched := range schedules {
			var bs nativemp3.BitStream
			nativemp3.BsInit(&bs, slab, len(slab))
			cr := newCgoBitReader(slab)
			defer cr.free()
			for _, n := range sched {
				got := nativemp3.GetBits(&bs, n)
				want := cr.getBits(n)
				assert.Equalf(t, want, got, "get_bits(%d) slab=% x", n, slab)
			}
			assert.Equalf(t, cr.pos(), bs.Pos, "final pos slab=% x sched=%v", slab, sched)
			assert.Equalf(t, cr.limit(), bs.Limit, "final limit slab=% x", slab)
		}
	}
}

func TestMatchFrameParity(t *testing.T) {
	requireStrict(t)
	const frameLen = 417
	hdr := []byte{0xFF, 0xFB, 0x90, 0x04}
	// A clean run of identical frames, and a truncated/garbled run.
	build := func(n, trailingGarbage int) []byte {
		buf := []byte{}
		for i := 0; i < n; i++ {
			f := make([]byte, frameLen)
			copy(f, hdr)
			buf = append(buf, f...)
		}
		buf = append(buf, make([]byte, trailingGarbage)...)
		return buf
	}
	cases := []struct {
		buf        []byte
		frameBytes int
	}{
		{build(nativemp3.MaxFrameSyncMatches+2, 0), frameLen},
		{build(3, 0), frameLen},
		{build(12, 7), frameLen},
		{build(2, 0), 200}, // wrong frame length: mismatch on second header
	}
	for i, c := range cases {
		assert.Equalf(t,
			cgoMatchFrame(c.buf, len(c.buf), c.frameBytes),
			nativemp3.Mp3dMatchFrame(c.buf, len(c.buf), c.frameBytes),
			"match_frame case %d", i)
	}
}

func TestFindFrameParity(t *testing.T) {
	requireStrict(t)
	const frameLen = 417
	hdr := []byte{0xFF, 0xFB, 0x90, 0x04}
	mkStream := func(leadGarbage, frames int) []byte {
		buf := make([]byte, leadGarbage)
		for i := 0; i < frames; i++ {
			f := make([]byte, frameLen)
			copy(f, hdr)
			buf = append(buf, f...)
		}
		return buf
	}
	streams := [][]byte{
		mkStream(0, nativemp3.MaxFrameSyncMatches+2),
		mkStream(1, nativemp3.MaxFrameSyncMatches+2),
		mkStream(37, nativemp3.MaxFrameSyncMatches+2),
		mkStream(3, 2), // too few frames to satisfy match -> falls through
		{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
	}
	for i, s := range streams {
		goFree, cFree := 0, 0
		var goFB int
		goOff := nativemp3.Mp3dFindFrame(s, len(s), &goFree, &goFB)
		cOff, cFB := cgoFindFrame(append([]byte(nil), s...), len(s), &cFree)
		assert.Equalf(t, cOff, goOff, "find_frame offset case %d", i)
		assert.Equalf(t, cFB, goFB, "find_frame frame_bytes case %d", i)
		assert.Equalf(t, cFree, goFree, "find_frame free_format_bytes case %d", i)
	}
}

func TestReservoirParity(t *testing.T) {
	requireStrict(t)
	type step struct {
		maindata       []byte // bytes already in the scratch maindata
		bsPos, bsLimit int    // bit cursor + limit prior to save
		payload        []byte // next frame payload
		mainDataBegin  int
	}
	steps := []step{
		{
			maindata:      []byte{0xDE, 0xAD, 0x11, 0x22, 0x33, 0x44, 0x55},
			bsPos:         16,
			bsLimit:       7 * 8,
			payload:       []byte{0xAA, 0xBB, 0xCC, 0xDD},
			mainDataBegin: 3,
		},
		{
			maindata:      []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A},
			bsPos:         3,
			bsLimit:       10 * 8,
			payload:       []byte{0xF0, 0xE1, 0xD2, 0xC3, 0xB4},
			mainDataBegin: 8,
		},
		{
			// main_data_begin larger than reserv -> restore returns false.
			maindata:      []byte{0x10, 0x20, 0x30, 0x40},
			bsPos:         8,
			bsLimit:       4 * 8,
			payload:       []byte{0x99, 0x88, 0x77},
			mainDataBegin: 200,
		},
	}
	for i, s := range steps {
		// Go side.
		var gdec nativemp3.Decoder
		var gscratch nativemp3.Scratch
		copy(gscratch.Maindata[:], s.maindata)
		gscratch.Bs.Pos = s.bsPos
		gscratch.Bs.Limit = s.bsLimit
		nativemp3.L3SaveReservoir(&gdec, &gscratch)

		var gbs nativemp3.BitStream
		nativemp3.BsInit(&gbs, s.payload, len(s.payload))
		gok := nativemp3.L3RestoreReservoir(&gdec, &gbs, &gscratch, s.mainDataBegin)
		gMain := append([]byte(nil), gscratch.Maindata[:gscratch.Bs.Limit/8]...)

		// C side.
		cr := newCgoReservoir()
		cr.save(s.maindata, s.bsPos, s.bsLimit)
		assert.Equalf(t, cr.reserv(), gdec.Reserv, "reserv after save case %d", i)
		assert.Equalf(t, cr.reservBuf(cr.reserv()), gdec.ReservBuf[:gdec.Reserv], "reserv_buf after save case %d", i)

		cMain, cLimit, cok := cr.restore(s.payload, s.mainDataBegin)
		assert.Equalf(t, cok, gok, "restore ok case %d", i)
		// L3_restore_reservoir writes the reassembled bit reader into the
		// SCRATCH (s->bs), leaving the frame bs untouched. Compare the C
		// scratch limit against the Go scratch limit (gscratch.Bs.Limit), not
		// the payload bs (gbs), which keeps its original payload length.
		assert.Equalf(t, cLimit, gscratch.Bs.Limit, "restore limit case %d", i)
		assert.Equalf(t, cMain, gMain, "restore maindata case %d", i)
	}
}
